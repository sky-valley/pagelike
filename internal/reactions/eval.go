package reactions

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/sky-valley/pagelike/internal/budget"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/query"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

// outcome ends a trigger or processor phase early: a thrown response
// (R-REACT-29..33) or a runtime error (R-REACT-34).
type outcome struct {
	thrown *thrown
	err    error
}

func (o outcome) done() bool { return o.thrown != nil || o.err != nil }

// runRules runs the matching rules of one phase in order (R-REACT-35); the
// first throw or error ends the phase. resp is the current response in the
// processor phase (nil for triggers); processors update it in place.
func (x *Reactions) runRules(ctx context.Context, st *reqState, rules []*rule, resp *response) outcome {
	for _, ru := range rules {
		if resp != nil && !ru.matchStatus(resp.status) {
			continue // the current status, after earlier processors (R-REACT-46)
		}
		if o := x.runRule(ctx, st, ru, resp); o.done() {
			return o
		}
	}
	return outcome{}
}

// runRule evaluates one Trigger or Processor: gates, then the selected
// actions in document order (R-REACT-17, R-REACT-18).
func (x *Reactions) runRule(ctx context.Context, st *reqState, ru *rule, resp *response) outcome {
	e := x.newEval(ctx, st, ru, resp)
	if resp != nil {
		e.exposeResponse() // gates, actions and dynamic values read it (R-REACT-26)
	}
	acts, actSlot := ru.actions, e.slot(SlotTriggerAction, SlotProcessorAction)
	if len(ru.when) > 0 {
		pass := true
		for _, g := range ru.when {
			v, err := e.code(ctx, e.slot(SlotTriggerWhen, SlotProcessorWhen), g.lang, g.source)
			if err != nil {
				return e.failure(g.lang, err)
			}
			if !sessel.Truthy(v) {
				pass = false
				break
			}
		}
		if !pass {
			acts, actSlot = ru.otherwise, SlotTriggerOtherwise
		}
	}
	for _, a := range acts {
		switch a.lang {
		case langSessel, langJS:
			if _, err := e.code(ctx, actSlot, a.lang, a.code); err != nil {
				return e.failure(a.lang, err)
			}
			if resp != nil && a.lang == langSessel {
				// Only Sessel assignments to status and body take effect;
				// JavaScript mutations are never read back (R-REACT-44/45).
				if err := e.readBack(); err != nil {
					return outcome{err: runtimeError(langSessel, err)}
				}
				e.exposeResponse()
			}
		case "http":
			req, lang, err := e.outbound(ctx, a.http)
			if err != nil {
				return e.failure(lang, err)
			}
			if req == nil {
				continue // dropped (R-REACT-4, R-REACT-49/50)
			}
			st.mu.Lock()
			n := len(st.queue)
			if n < MaxQueued {
				st.queue = append(st.queue, req)
			}
			st.mu.Unlock()
			if n >= MaxQueued {
				return outcome{err: runtimeError(langSessel, fmt.Errorf("more than %d outbound requests queued by one request", MaxQueued))}
			}
		}
	}
	return outcome{}
}

// evalEnv evaluates the code of one rule.
type evalEnv struct {
	x    *Reactions
	st   *reqState
	ru   *rule
	resp *response
	host *query.Host
	vars map[string]sessel.Value
	verr error
	done bool // vars computed

	headers    *sessel.Dict // Context.response.headers as exposed
	headerSnap []string     // its entries when exposed
}

func (x *Reactions) newEval(ctx context.Context, st *reqState, ru *rule, resp *response) *evalEnv {
	e := &evalEnv{x: x, st: st, ru: ru, resp: resp}
	e.refresh(ctx)
	return e
}

// refresh binds the host to the site's current state, so store queries and
// self see side-effect writes made earlier in the request.
func (e *evalEnv) refresh(ctx context.Context) {
	snap, err := e.st.site.Index(ctx)
	if err != nil {
		return
	}
	h := query.NewHost(e.st.site, snap)
	h.WriteProvider = &writer{x: e.x, st: e.st, env: e}
	e.host = h
}

// snap is the snapshot the evaluation reads (nil if the index failed).
func (e *evalEnv) snap() *site.Snapshot {
	if e.host == nil {
		return nil
	}
	return e.host.Snap
}

// sesselEnv is the evaluation context of trigger and processor Sessel
// (R-REACT-22, R-REACT-24): self is the target document as currently
// stored, prior as stored at arrival, Context the request's shared object.
func (e *evalEnv) sesselEnv(ctx context.Context) (*sessel.Env, error) {
	vars, err := e.bindings(ctx)
	if err != nil {
		return nil, err
	}
	env := &sessel.Env{Host: e.host, HasSelf: true, Context: e.st.cctx, Request: e.st.req, Vars: vars, Budget: budget.From(ctx).Sessel()}
	if e.host != nil {
		if d := e.host.Document(e.st.docPath); d != nil {
			if el := d.Element(); el != nil {
				env.Self = el
			}
		}
	}
	if e.st.prior != nil {
		env.Prior = e.st.prior.Element()
	}
	return env, nil
}

// slot names the JavaScript slot of this rule's kind: a trigger's, or a
// processor's (processors run with a response).
func (e *evalEnv) slot(trigger, processor string) string {
	if e.resp != nil {
		return processor
	}
	return trigger
}

// code evaluates a Sessel program or a JavaScript module in the given slot.
func (e *evalEnv) code(ctx context.Context, slot, lang, src string) (sessel.Value, error) {
	switch lang {
	case langSessel:
		env, err := e.sesselEnv(ctx)
		if err != nil {
			return nil, err
		}
		return sessel.Eval(ctx, src, env)
	case langJS:
		return runJS(ctx, e.st.site, e.snap(), slot, src, e.jsArg())
	}
	return nil, fmt.Errorf("unsupported language")
}

// value evaluates a property value to a string (R-REACT-3, R-REACT-55): a
// null result counts as absent, other values use their string form.
func (e *evalEnv) value(ctx context.Context, v *value) (s string, present bool, err error) {
	if v == nil {
		return "", false, nil
	}
	switch v.lang {
	case langStatic:
		return v.text, true, nil
	case langOther:
		return "", false, nil
	}
	r, err := e.code(ctx, SlotHTTPRequestProperty, v.lang, v.source)
	if err != nil || r == nil {
		return "", false, err
	}
	if s, ok := r.(string); ok {
		return s, true, nil
	}
	return sessel.TextOf(r), true, nil
}

// failure turns an evaluation error into the phase's outcome: a thrown
// HTTPResponse is the response, anything else a runtime error.
func (e *evalEnv) failure(lang string, err error) outcome {
	if r, ok := sessel.ResponseOf(err); ok {
		t := &thrown{status: r.Status, headers: r.Headers}
		if r.HasBody || r.HasMessage {
			// Sessel-thrown bodies go through HTML serialization (R-REACT-32).
			t.body = []byte(r.HTMLBody())
		}
		return outcome{thrown: t}
	}
	var tr *ThrownResponse
	if asErr(err, &tr) {
		t := &thrown{status: tr.Status, headers: tr.Headers}
		switch {
		case tr.HasBody:
			t.body = []byte(tr.Body) // byte-for-byte for JavaScript
		case tr.HasMessage:
			t.body = []byte(tr.Message)
		}
		return outcome{thrown: t}
	}
	return outcome{err: runtimeError(lang, err)}
}

// ---------------------------------------------------------------- processor response

// exposeResponse installs Context.response from the current response.
func (e *evalEnv) exposeResponse() {
	sessel.SetResponse(e.st.cctx, e.resp.status, string(e.resp.body), e.resp.header)
	e.headers, e.headerSnap = nil, nil
	if rd, _ := e.st.cctx.Lookup("response").(*sessel.Dict); rd != nil {
		if hd, ok := rd.Lookup("headers").(*sessel.Dict); ok {
			e.headers = hd
			for _, k := range hd.Keys() {
				e.headerSnap = append(e.headerSnap, k+"\x00"+sessel.TextOf(hd.Lookup(k)))
			}
		}
	}
}

// readBack applies Sessel assignments to Context.response.status and
// .body (R-REACT-44). Response headers are not writable (R-REACT-45): a
// replaced map is ignored with a warning, an index assignment into the
// exposed map is a runtime error.
func (e *evalEnv) readBack() error {
	rd, _ := e.st.cctx.Lookup("response").(*sessel.Dict)
	if rd == nil {
		return nil
	}
	if hd, ok := rd.Lookup("headers").(*sessel.Dict); !ok || hd != e.headers {
		e.x.log.Warn("reactions: a processor assigned Context.response.headers; header writes have no effect (throw an HTTPResponse to set headers)", "processor", e.ru.path)
	} else {
		var now []string
		for _, k := range hd.Keys() {
			now = append(now, k+"\x00"+sessel.TextOf(hd.Lookup(k)))
		}
		if strings.Join(now, "\x01") != strings.Join(e.headerSnap, "\x01") {
			return fmt.Errorf("invalid assignment target: Context.response.headers entries cannot be assigned (throw an HTTPResponse to set headers)")
		}
	}
	switch s := rd.Lookup("status").(type) {
	case int64:
		if s < 100 || s > 599 {
			return fmt.Errorf("Context.response.status must be an integer from 100 to 599, not %d", s)
		}
		e.resp.status = int(s)
	case float64:
		if s != float64(int64(s)) || s < 100 || s > 599 {
			return fmt.Errorf("Context.response.status must be an integer from 100 to 599, not %v", s)
		}
		e.resp.status = int(s)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < 100 || n > 599 {
			return fmt.Errorf("Context.response.status must be an integer from 100 to 599, not %q", s)
		}
		e.resp.status = n
	default:
		return fmt.Errorf("Context.response.status must be an integer from 100 to 599, not %s", sessel.TypeName(s))
	}
	var body string
	switch b := rd.Lookup("body").(type) {
	case string:
		body = b
	default:
		body = sessel.TextOf(b)
	}
	if body != string(e.resp.body) {
		e.resp.body = []byte(body)
		e.resp.changed = true
	}
	return nil
}

// ---------------------------------------------------------------- JavaScript context

// jsArg is the JavaScript ctx (R-REACT-25): ctx.request, plus ctx.response
// for processors. Writes to it have no effect.
func (e *evalEnv) jsArg() map[string]any {
	st := e.st
	r := st.r
	headers := map[string]any{}
	for k, v := range contextHeaders(r) {
		sep := ", "
		if strings.EqualFold(k, "Cookie") {
			sep = "; "
		}
		headers[strings.ToLower(k)] = strings.Join(v, sep)
	}
	q := map[string]any{}
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			q[k] = v[0]
		}
	}
	// Anonymous: no claims and no roles, not null (R-REACT-25, live
	// 2026-09-29).
	auth := map[string]any{"claims": map[string]any{}, "roles": []any{}}
	if p := st.principal; p != nil && p.Authenticated {
		roles := make([]any, 0, len(p.Roles))
		for _, x := range p.Roles {
			roles = append(roles, x)
		}
		auth = map[string]any{"username": username(p), "claims": claimsOf(p), "role": roles, "roles": roles}
	}
	body, _ := st.req.Lookup("body").(string)
	arg := map[string]any{"request": map[string]any{
		"method": strings.ToUpper(st.method), "path": st.path, "host": st.call.Host,
		"headers": headers, "query": q, "body": body, "auth": auth,
	}}
	if e.resp != nil {
		h := map[string]any{}
		for k, v := range e.resp.header {
			h[strings.ToLower(k)] = strings.Join(v, ", ")
		}
		arg["response"] = map[string]any{"status": e.resp.status, "body": string(e.resp.body), "headers": h}
	}
	return arg
}

// ---------------------------------------------------------------- bindings

// bindings evaluates the rule's expression bindings once (R-REACT-24):
// e: Sessel expressions, j: JavaScript expressions. Resource bindings (r:)
// are not bound (live 2026-09-29).
func (e *evalEnv) bindings(ctx context.Context) (map[string]sessel.Value, error) {
	if e.done {
		return e.vars, e.verr
	}
	e.done = true
	if len(e.ru.bindings) == 0 {
		return nil, nil
	}
	vars := map[string]sessel.Value{}
	for _, b := range e.ru.bindings {
		var v sessel.Value
		var err error
		switch b.kind {
		case "sessel":
			env := &sessel.Env{Host: e.host, Context: e.st.cctx, Request: e.st.req, Vars: vars, Budget: budget.From(ctx).Sessel()}
			v, err = sessel.Eval(ctx, b.src, env)
		case "js":
			v, err = runJS(ctx, e.st.site, e.snap(), SlotBinding, "export default function(ctx) { return ("+b.src+"); }", e.jsArg())
		}
		if err != nil {
			e.verr = fmt.Errorf("binding %s: %w", b.name, err)
			return nil, e.verr
		}
		vars[b.name] = v
	}
	e.vars = vars
	return vars, nil
}

// ---------------------------------------------------------------- errors

// runtimeError is the failure of a trigger or processor (R-REACT-34): 500
// (501 without a JavaScript runtime) with an error document that embeds a
// BindingFailure item.
func runtimeError(lang string, err error) *errdoc.Error {
	language := TypeSessel
	if lang == langJS {
		language = TypeJavaScript
	}
	variant, msg, stack := "threw", err.Error(), ""
	var je *JSError
	switch {
	case asErr(err, &je):
		variant, msg, stack = je.Variant, je.Message, je.Stack
	case sessel.IsParseError(err):
		variant = "parse"
	default:
		if se, ok := sessel.AsError(err); ok {
			msg = se.Error()
			switch se.Reason {
			case sessel.ReasonTimeout:
				variant = "timeout"
			case sessel.ReasonBudget, sessel.ReasonDepth:
				variant = "out-of-memory"
			}
		}
	}
	status := http.StatusInternalServerError
	if lang == langJS {
		status = jsStatus(err)
	}
	return &errdoc.Error{Status: status, Kind: "BindingFailure", Message: "a reaction failed: " + msg,
		Detail: bindingFailure(language, variant, msg, stack)}
}

func bindingFailure(language, variant, msg, stack string) string {
	var b strings.Builder
	b.WriteString(`<div itemscope itemtype="` + TypeBindingFailure + `">`)
	b.WriteString(`<meta itemprop="language" content="` + attrEsc(language) + `">`)
	b.WriteString(`<meta itemprop="variant" content="` + attrEsc(variant) + `">`)
	b.WriteString(`<p itemprop="message">` + textEsc(msg) + `</p>`)
	if stack != "" {
		b.WriteString(`<pre itemprop="stack">` + textEsc(stack) + `</pre>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}
