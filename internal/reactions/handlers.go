package reactions

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/budget"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/microdata"
	"github.com/sky-valley/pagelike/internal/query"
	"github.com/sky-valley/pagelike/internal/sessel"
)

// afterWrite is the engine AfterWrite hook. Inside the write's transaction
// it records the requests the trigger phase queued (so they are durable
// with the write, R-REACT-59) and the deliveries of transition handlers
// (R-REACT-78). Delivery never fails the write; a handler condition that
// cannot be judged refuses it before it commits (R-REACT-79).
func (x *Reactions) afterWrite(ctx context.Context, w *engine.WriteCtx, res *engine.Result) error {
	st := stateFrom(ctx)
	state := statePending
	if st != nil {
		state = stateHeld // released once the response is written
	}
	if st.isMain(w) {
		st.mu.Lock()
		q := append([]*outRequest(nil), st.queue[st.persisted:]...)
		st.mu.Unlock()
		if len(q) > 0 {
			ids, err := insertTx(w.Tx, q, stateHeld)
			if err != nil {
				x.log.Error("reactions: recording queued requests", "err", err, "path", w.Op.Path)
			} else {
				st.mu.Lock()
				st.persisted += len(q)
				st.mu.Unlock()
				st.addTentative(ids...)
			}
		}
	}
	if w.Op.Plane == engine.Authoring || w.Snap == nil {
		return nil // WebDAV edits never fire handlers (R-REACT-75)
	}
	ix := indexFor(w.Snap)
	if len(ix.handlers) == 0 {
		return nil
	}
	cs, ok := changes(w, ix.types)
	if !ok {
		return nil
	}
	var reqs []*outRequest
	var owed notified
	for _, h := range ix.handlers {
		for _, p := range cs.pairs {
			if p.new == nil || !h.accepts(p.new) {
				continue // exits never fire
			}
			vn := stateOf(p.new, h.property)
			if len(vn) != 1 {
				continue // absent or multi-valued: no firing (R-REACT-66)
			}
			so, soOK := single(stateOf(p.old, h.property))
			if soOK && so == vn[0] || !containsStr(h.becomes, vn[0]) {
				continue
			}
			doc, bodyEl, sel := transitionDocument(w.Op.Path, p.new, ix.types)
			if h.when != nil {
				pass, err := x.handlerGate(ctx, w, h, doc, bodyEl)
				if err != nil {
					return x.gateFailed(w, h, err)
				}
				if !pass {
					continue
				}
			}
			for _, a := range h.actions {
				r := x.delivery(st, w, a, doc)
				if r == nil {
					continue
				}
				r.Key = w.Op.Path + "\x00" + sel + "\x00" + r.Method + " " + r.URL
				if err := owed.add(w.Op.Path, sel, r); err != nil {
					x.log.Warn("reactions: a save's transition notifications pass a size limit; the save is refused", "handler", h.path, "path", w.Op.Path)
					return err
				}
				reqs = append(reqs, r)
			}
		}
	}
	if len(reqs) == 0 {
		return nil
	}
	ids, err := insertTx(w.Tx, reqs, state)
	if err != nil {
		x.log.Error("reactions: recording transition deliveries", "err", err, "path", w.Op.Path)
		return nil
	}
	if st != nil {
		st.addTentative(ids...)
	} else {
		// A write outside a public-plane request: the rows become visible
		// at commit; the woken worker keeps polling while it is young.
		site := w.Site
		time.AfterFunc(50*time.Millisecond, func() { x.out.wake(site) })
	}
	return nil
}

// accepts reports whether a qualifying type branch accepts the item: only
// its type is judged (R-REACT-77).
func (h *handler) accepts(n *html.Node) bool {
	for _, b := range h.branches {
		if b.Matches(n) {
			return true
		}
	}
	return false
}

// handlerGate evaluates a handler's when condition against the Transition
// item (R-REACT-79): Sessel sees it as self, a JavaScript module gets it as
// its first argument. The result is judged by Sessel's rule in both
// languages. An error means the condition cannot be judged.
func (x *Reactions) handlerGate(ctx context.Context, w *engine.WriteCtx, h *handler, doc string, bodyEl *html.Node) (bool, error) {
	root, err := dom.Parse([]byte(doc))
	if err != nil {
		return false, err
	}
	body := dom.Body(root)
	if body == nil {
		body = dom.DocumentElement(root) // documents need not have a body (LO-15)
	}
	if body == nil {
		return false, errors.New("the Transition document has no item")
	}
	if h.when.lang == langJS {
		it := microdata.Parse(body)
		arg := map[string]any{"path": propText(it, "path"), "selector": propText(it, "selector"), "body": dom.OuterHTML(bodyEl)}
		v, err := runJS(ctx, w.Site, w.Snap, SlotTriggerWhen, h.when.source, arg, nil, nil)
		if err != nil {
			return false, err
		}
		return sessel.Truthy(v), nil
	}
	d := &sessel.Document{Root: root}
	env := &sessel.Env{Host: query.NewHost(w.Site, w.Snap), Self: sessel.Queried(body, d), HasSelf: true, Budget: budget.From(ctx).Sessel()}
	v, err := sessel.Eval(ctx, h.when.source, env)
	if err != nil {
		return false, err
	}
	return sessel.Truthy(v), nil
}

// gateFailed refuses a save whose handler condition could not be judged
// (R-REACT-79, docs snapshot 2026-11-23): running out of budget keeps its
// 503; anything else is a 500 that names no handler and repeats nothing the
// condition said, because that can hold data the writer may not read. The
// log names the handler.
func (x *Reactions) gateFailed(w *engine.WriteCtx, h *handler, err error) error {
	x.log.Warn("reactions: transition handler condition failed; the save is refused", "err", err, "handler", h.path, "path", w.Op.Path)
	if ed := runtimeError(h.when.lang, err); ed.Status == http.StatusServiceUnavailable {
		return ed
	}
	const msg = "The when condition of a transition handler could not be evaluated, so nothing was saved."
	doc := "<!DOCTYPE html>\n<html><body>\n<article itemscope itemtype=\"https://pagelove.org/Error/Internal\">\n" +
		"  <meta itemprop=\"status\" content=\"500\">\n  <p itemprop=\"message\">" + msg + "</p>\n" +
		"  <meta itemprop=\"resource\" content=\"" + attrEsc(w.Op.Path) + "\">\n" +
		"  <div itemprop=\"problem\" itemscope itemtype=\"https://pagelove.org/Problem/TransitionGateFailed\"></div>\n</article>\n</body></html>\n"
	return &errdoc.Error{Status: http.StatusInternalServerError, Kind: "TransitionGateFailed", Message: msg, Document: doc}
}

// delivery builds one handler delivery: static url, method and headers;
// the body is always the Transition document as text/html (R-REACT-80).
func (x *Reactions) delivery(st *reqState, w *engine.WriteCtx, a *httpAction, doc string) *outRequest {
	if a.url == nil || a.url.dynamic() {
		return nil
	}
	origin := ""
	if st != nil {
		origin = st.origin
	} else if x.srv != nil && w.Site != nil {
		origin = "http://" + w.Site.Name + "." + x.srv.Cfg.Domain
	}
	target, ok := resolveURL(a.url.text, origin)
	if !ok {
		return nil
	}
	method := "POST"
	if a.method != nil {
		method = strings.ToUpper(strings.TrimSpace(a.method.text))
	}
	if !outboundMethods[method] {
		return nil
	}
	headers := [][2]string{{"Content-Type", "text/html"}}
	for _, hd := range a.headers {
		v := ""
		if hd.value != nil {
			v = hd.value.text
		}
		if hd.key == "" || !validHeaderName(hd.key) || !validHeaderValue(v) {
			continue
		}
		switch strings.ToLower(hd.key) {
		case "host", "content-length", "transfer-encoding", "connection", "content-type":
			continue
		}
		headers = append(headers, [2]string{hd.key, v})
	}
	return &outRequest{Kind: kindTransition, URL: target, Method: method, Header: headers, Body: []byte(doc), Attempts: transitionAttempts}
}

// transitionDocument renders the Transition document of a changed item
// (R-REACT-81) and returns it with the delivered copy of the item.
func transitionDocument(path string, item *html.Node, types *typeInfo) (string, *html.Node, string) {
	t := firstType(item)
	sel := `[itemtype="` + cssString(t) + `"]`
	if k := types.keyOf(t); k != "" {
		if v := keyValue(item, k); v != "" {
			sel += `:has([itemprop="` + cssString(k) + `"]:value-equals("` + cssString(v) + `"))`
		}
	}
	body := dom.Clone(item)
	dom.SetAttr(body, "itemprop", "body")
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html>\n  <head><title>Transition</title></head>\n")
	b.WriteString(`  <body itemscope itemtype="` + TypeTransition + `">` + "\n")
	b.WriteString(`    <meta itemprop="path" content="` + attrEsc(path) + `">` + "\n")
	b.WriteString(`    <meta itemprop="selector" content="` + attrEsc(sel) + `">` + "\n")
	b.WriteString("    " + dom.OuterHTML(body) + "\n")
	b.WriteString("  </body>\n</html>\n")
	return b.String(), body, sel
}

// cssString escapes a value for a double-quoted CSS string.
func cssString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

// Limits on what one save may notify (R-REACT-86b, docs snapshot
// 2026-11-23): the notifications are stored with the save, so they have a
// size.
const (
	maxNotification    = 256 << 10 // delivery header and Transition document together
	maxDeliveryHeader  = 64 << 10
	maxNotifiedBytes   = 1 << 20
	maxNotifiedPerSave = 1000
)

// notified totals the notifications a save owes.
type notified struct {
	count int
	bytes int
}

// add counts one notification and refuses the save at the first limit it
// reaches. The problem names no handler: the handler's page is another
// page.
func (n *notified) add(path, sel string, r *outRequest) error {
	header := len(r.Method) + len(r.URL)
	for _, kv := range r.Header {
		header += len(kv[0]) + len(kv[1]) + 4
	}
	size := header + len(r.Body)
	n.count++
	n.bytes += size
	switch {
	case header > maxDeliveryHeader:
		return transitionLimit(path, "header", maxDeliveryHeader, header, sel,
			fmt.Sprintf("The delivery header of a notification this write would send is larger than %d bytes, so nothing was saved.", maxDeliveryHeader))
	case size > maxNotification:
		return transitionLimit(path, "notification", maxNotification, size, sel,
			fmt.Sprintf("A notification this write would send is larger than %d bytes, so nothing was saved.", maxNotification))
	case n.bytes > maxNotifiedBytes:
		return transitionLimit(path, "save-bytes", maxNotifiedBytes, n.bytes, "",
			fmt.Sprintf("The notifications this write would send are larger than %d bytes in all, so nothing was saved.", maxNotifiedBytes))
	case n.count > maxNotifiedPerSave:
		return transitionLimit(path, "save-count", maxNotifiedPerSave, n.count, "",
			fmt.Sprintf("This write would send more than %d notifications, so nothing was saved.", maxNotifiedPerSave))
	}
	return nil
}

func transitionLimit(path, limit string, maxSize, size int, sel, msg string) error {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html><body>\n<article itemscope itemtype=\"https://pagelove.org/Error/ValidationFailed\">\n")
	b.WriteString("  <meta itemprop=\"status\" content=\"422\">\n  <p itemprop=\"message\">" + msg + "</p>\n")
	b.WriteString("  <meta itemprop=\"resource\" content=\"" + attrEsc(path) + "\">\n")
	b.WriteString("  <div itemprop=\"problem\" itemscope itemtype=\"https://pagelove.org/Problem/TransitionLimit\">\n")
	fmt.Fprintf(&b, "    <meta itemprop=\"limit\" content=\"%s\">\n    <meta itemprop=\"max\" content=\"%d\">\n    <meta itemprop=\"size\" content=\"%d\">\n", limit, maxSize, size)
	if sel != "" {
		b.WriteString("    <meta itemprop=\"selector\" content=\"" + attrEsc(sel) + "\">\n")
	}
	b.WriteString("  </div>\n</article>\n</body></html>\n")
	return &errdoc.Error{Status: http.StatusUnprocessableEntity, Kind: "ValidationFailed", Message: msg, Document: b.String()}
}
