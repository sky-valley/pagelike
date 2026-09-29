package reactions

import (
	"context"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/budget"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/query"
	"github.com/sky-valley/pagelike/internal/sessel"
)

// afterWrite is the engine AfterWrite hook. Inside the write's transaction
// it records the requests the trigger phase queued (so they are durable
// with the write, R-REACT-59) and the deliveries of transition handlers
// (R-REACT-78). It never fails the write: nothing a handler does can.
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
			doc, bodyEl := transitionDocument(w.Op.Path, p.new, ix.types)
			if h.when != nil && !x.handlerGate(ctx, w, h, doc, bodyEl) {
				continue
			}
			for _, a := range h.actions {
				if r := x.delivery(st, w, a, doc); r != nil {
					reqs = append(reqs, r)
				}
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

// handlerGate evaluates a handler's Sessel when gate against the
// Transition item (R-REACT-79): falsy or failing means no delivery.
func (x *Reactions) handlerGate(ctx context.Context, w *engine.WriteCtx, h *handler, doc string, _ *html.Node) bool {
	root, err := dom.Parse([]byte(doc))
	if err != nil {
		return false
	}
	body := dom.Body(root)
	if body == nil {
		body = dom.DocumentElement(root) // documents need not have a body (LO-15)
	}
	if body == nil {
		return false
	}
	d := &sessel.Document{Root: root}
	env := &sessel.Env{Host: query.NewHost(w.Site, w.Snap), Self: sessel.Queried(body, d), HasSelf: true, Budget: budget.From(ctx).Sessel()}
	v, err := sessel.Eval(ctx, h.when.source, env)
	if err != nil {
		x.log.Info("reactions: transition handler gate failed", "err", err, "handler", h.path)
		return false
	}
	return sessel.Truthy(v)
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
	return &outRequest{Kind: kindTransition, URL: target, Method: method, Header: headers, Body: []byte(doc), Attempts: 1}
}

// transitionDocument renders the Transition document of a changed item
// (R-REACT-81) and returns it with the delivered copy of the item.
func transitionDocument(path string, item *html.Node, types *typeInfo) (string, *html.Node) {
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
	return b.String(), body
}

// cssString escapes a value for a double-quoted CSS string.
func cssString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
