package liquid

import (
	"bytes"
	"container/list"
	"context"
	crand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/osteele/liquid/parser"
	"github.com/osteele/liquid/render"
	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/xmldom"
)

// Options configures an Engine. Zero fields take defaults.
type Options struct {
	// Now is the clock for "now" and "today"; default time.Now.
	Now func() time.Time
	// Rand is the entropy for random, diceware and salts; default
	// crypto/rand.
	Rand io.Reader
	// Limits are the budget limits of a render given a nil *Budget.
	Limits Limits
	// MaxExpDepth bounds the nesting of *_exp filters; default 32, the
	// documented PageLove value (R-LIQ-163).
	MaxExpDepth int
	// MaxRandomLength caps `random`'s length; default 4096.
	MaxRandomLength int
	// MaxBcryptCost caps bcrypt's cost; default 16.
	MaxBcryptCost int
	// MaxArgon2Memory (KiB) and MaxArgon2Time cap argon2's parameters;
	// defaults 262144 and 10.
	MaxArgon2Memory int64
	MaxArgon2Time   int64
	// WordList is diceware's word list; default pagelike's list.
	WordList []string
	// CacheSize is the number of compiled templates kept; default 512.
	CacheSize int
	// AutoEscape HTML-escapes every output ({{ }}, echo) whose value is not
	// safe (see SafeString), as PageLove does (live 2026-09-29: a query
	// value "<b>x</b>" and newline_to_br's "<br />" come out escaped;
	// harness/observations/live-2026-09-29-serialize/liquid-escape-*.json).
	// Outputs in a raw-text host (script, style, …) are never escaped.
	AutoEscape bool
}

func (o *Options) defaults() {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Rand == nil {
		o.Rand = crand.Reader
	}
	o.Limits = o.Limits.withDefaults()
	if o.MaxExpDepth == 0 {
		o.MaxExpDepth = 32
	}
	if o.MaxRandomLength == 0 {
		o.MaxRandomLength = 4096
	}
	if o.MaxBcryptCost == 0 {
		o.MaxBcryptCost = 16
	}
	if o.MaxArgon2Memory == 0 {
		o.MaxArgon2Memory = 262144
	}
	if o.MaxArgon2Time == 0 {
		o.MaxArgon2Time = 10
	}
	if len(o.WordList) == 0 {
		o.WordList = defaultWordList
	}
	if o.CacheSize == 0 {
		o.CacheSize = 512
	}
}

// Host describes the element that carries the template: its name, and
// whether the document is XML. The content of an HTML raw-text host
// (title, script, style, textarea, …) is not a place for markup, so failed
// outputs there render nothing (R-LIQ-201); XML documents use the XML
// notion of markup context. For a named HTML host (HostOf), character
// references inside Liquid markup are decoded where the document's parser
// would decode them (R-LIQ-13); Host{} (Render) leaves them alone.
type Host struct {
	Tag string
	XML bool
}

// HostOf describes a template host element.
func HostOf(n *html.Node) Host {
	if n == nil {
		return Host{}
	}
	return Host{Tag: n.Data, XML: xmldom.IsXML(n)}
}

// Engine renders PageLove-flavoured Liquid. It is safe for concurrent use;
// make one per process. Compiled templates are cached (LRU) and shared by
// all renders.
type Engine struct {
	opts  Options
	pool  sync.Pool // *instance
	cache *lru      // compiled templates
	preds *lru      // rewritten *_exp predicates
}

// New creates an Engine.
func New(opts Options) *Engine {
	opts.defaults()
	e := &Engine{opts: opts, cache: newLRU(opts.CacheSize), preds: newLRU(opts.CacheSize)}
	e.pool.New = func() any { return e.newInstance() }
	return e
}

// instance is a render configuration whose filters are bound to the state
// of the render that has it checked out. Filters get no context from the
// library, so per-render state has to reach them this way.
type instance struct {
	eng *Engine
	cfg render.Config
	st  *renderState
}

func (e *Engine) newInstance() *instance {
	inst := &instance{eng: e, cfg: render.NewConfig()}
	inst.cfg.TemplateStore = denyStore{}
	addTags(&inst.cfg)
	inst.addFilters()
	return inst
}

// denyStore is the template store: templates perform no I/O. The library's
// default store reads the server's working directory (decision 0002).
type denyStore struct{}

func (denyStore) ReadTemplate(string) ([]byte, error) {
	return nil, errors.New("templates cannot read files")
}

// renderState is the state of one render.
type renderState struct {
	opts    *Options
	budget  *Budget
	ctx     context.Context
	now     time.Time
	xml     bool
	rawHost bool // the template's host holds raw text
	charges int

	depth         int
	depthExceeded bool

	cycles   map[string]int
	counters map[string]int
	stored   map[string]int64 // bytes charged per assigned variable
}

// charge spends work units; the request context is checked every 64
// charges.
func (st *renderState) charge(n int64) error {
	if err := st.budget.Charge(n); err != nil {
		return err
	}
	if st.charges++; st.charges&63 == 0 {
		return st.ctx.Err()
	}
	return nil
}

func (st *renderState) chargeMemory(n int64) error { return st.budget.ChargeMemory(n) }

// store charges the memory an assigned value holds. Reassigning a variable
// charges only growth, so the `push` idiom stays linear.
func (st *renderState) store(name string, size int64) error {
	prev, seen := st.stored[name]
	if !seen {
		st.stored[name] = 0
	}
	if size <= prev {
		return nil
	}
	st.stored[name] = size
	return st.chargeMemory(size - prev)
}

// fatal returns the error that must fail the render, if any: budget
// exhaustion or a cancelled request.
func (st *renderState) fatal(err error) error {
	if berr := st.budget.Err(); berr != nil {
		return berr
	}
	if isFatal(err) {
		return err
	}
	if cerr := st.ctx.Err(); cerr != nil {
		return cerr
	}
	return nil
}

// Render renders an HTML template: src is the host element's raw inner
// markup (see the package documentation). vars are the variables in scope
// (bindings, `request`). budget is the request's budget; nil gives the
// render its own budget with the engine's default limits.
func (e *Engine) Render(ctx context.Context, src string, vars map[string]any, budget *Budget) (string, error) {
	return e.RenderFor(ctx, Host{}, src, vars, budget)
}

// RenderFor renders a template whose host is described by host.
func (e *Engine) RenderFor(ctx context.Context, host Host, src string, vars map[string]any, budget *Budget) (out string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if budget == nil {
		budget = NewBudget(e.opts.Limits)
	}
	if err := budget.Err(); err != nil {
		return "", &Error{Kind: KindBudget, Message: err.Error(), err: err}
	}
	node, err := e.compile(src, host)
	if err != nil {
		return "", err
	}

	inst := e.pool.Get().(*instance)
	st := &renderState{
		opts: &e.opts, budget: budget, ctx: ctx,
		now:     budget.requestNow(e.opts.Now),
		xml:     host.XML,
		rawHost: RawTextHost(host.Tag),
		cycles:  map[string]int{}, counters: map[string]int{}, stored: map[string]int64{},
	}
	inst.st = st
	defer func() {
		inst.st = nil
		e.pool.Put(inst)
	}()
	defer func() {
		if p := recover(); p != nil {
			// The library lets some panics escape; never let them take
			// down the server, and never show a stack trace.
			out, err = "", e.fail(st, panicError(p))
		}
	}()

	bindings := make(map[string]any, len(vars)+3)
	bindings["blank"] = emptyLiteral{blank: true}
	bindings["empty"] = emptyLiteral{}
	for k, v := range vars {
		if i, ok := v.(int64); ok {
			v = mkInt(i) // the library indexes and ranges with int only
		}
		bindings[k] = v
	}
	bindings[stateKey] = st

	var buf bytes.Buffer
	if rerr := render.Render(node, &memWriter{w: &buf, st: st}, bindings, inst.cfg); rerr != nil {
		return "", e.fail(st, rerr)
	}
	if berr := budget.Err(); berr != nil {
		return "", e.fail(st, berr)
	}
	return buf.String(), nil
}

// fail maps a render failure to an *Error.
func (e *Engine) fail(st *renderState, err error) *Error {
	line := 0
	var pe parser.Error
	if errors.As(err, &pe) {
		line = pe.LineNumber()
	}
	if berr := st.budget.Err(); berr != nil {
		return &Error{Kind: KindBudget, Message: berr.Error(), Line: line, err: berr}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Kind: KindBudget, Message: "time limit exceeded", Line: line, err: fmt.Errorf("%w: %w", ErrBudget, err)}
	}
	return &Error{Kind: KindTemplate, Message: templateMessage(err), Line: line, err: err}
}

// templateMessage is the message of a composition error.
func templateMessage(err error) string {
	switch {
	case errors.Is(err, errBreak):
		return errBreak.Error()
	case errors.Is(err, errContinue):
		return errContinue.Error()
	}
	return errMessage(err)
}

// compile preprocesses and compiles a template, through the cache. Syntax
// errors are cached too, so a broken page does not recompile per request.
func (e *Engine) compile(src string, host Host) (render.Node, error) {
	key := cacheKey(src, host)
	if v, ok := e.cache.get(key); ok {
		if err, isErr := v.(*Error); isErr {
			return nil, err
		}
		return v.(render.Node), nil
	}
	node, err := e.compileUncached(src, host)
	if err != nil {
		e.cache.put(key, err)
		return nil, err
	}
	e.cache.put(key, node)
	return node, nil
}

func (e *Engine) compileUncached(src string, host Host) (render.Node, *Error) {
	pre, err := preprocess(src, host)
	if err != nil {
		var le *Error
		if errors.As(err, &le) {
			return nil, le
		}
		return nil, &Error{Kind: KindTemplate, Message: cleanMessage(err), err: err}
	}
	inst := e.pool.Get().(*instance)
	defer e.pool.Put(inst)
	node, perr := inst.cfg.Compile(pre, parser.SourceLoc{LineNo: 1})
	if perr != nil {
		return nil, &Error{Kind: KindTemplate, Message: compileMessage(perr), Line: perr.LineNumber(), err: perr}
	}
	return node, nil
}

func cacheKey(src string, host Host) string {
	tag := ""
	if !host.XML && rawTextElements[strings.ToLower(host.Tag)] {
		tag = strings.ToLower(host.Tag)
	}
	return fmt.Sprintf("%t\x00%t\x00%s\x00%s", host.XML, host.Tag != "", tag, src)
}

// compileMessage cleans a parser error: the library appends the offending
// token as " in {% … %}", which after preprocessing is a pl_out tag.
func compileMessage(err error) string {
	msg := cleanMessage(err)
	if i := strings.LastIndex(msg, " in {%"); i >= 0 {
		tok := msg[i+4:]
		if body, ok := strings.CutPrefix(strings.TrimLeft(tok[2:], "- "), "pl_out"); ok {
			body = strings.TrimPrefix(body, "_attr")
			body = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(body), "%}"), "-"))
			msg = msg[:i] + " in {{ " + decodeArgs(body) + " }}"
		}
	}
	return strings.Join(strings.Fields(msg), " ")
}

// predicate rewrites an expression filter's predicate string (pl_pred),
// caching the result. A predicate that does not parse is a filter error.
func (e *Engine) predicate(src string) (any, error) {
	if v, ok := e.preds.get(src); ok {
		if err, isErr := v.(error); isErr {
			return nil, err
		}
		return v, nil
	}
	out := rewriteCond(src)
	var result any = out
	if err := parseExpression(out); err != nil {
		result = fmt.Errorf("invalid expression %q: %s", clipString(src, 80), cleanMessage(err))
	}
	e.preds.put(src, result)
	if err, isErr := result.(error); isErr {
		return nil, err
	}
	return result, nil
}

// lru is a small mutex-guarded LRU cache.
type lru struct {
	mu    sync.Mutex
	size  int
	order *list.List // front = most recent; values are *lruEntry
	items map[string]*list.Element
}

type lruEntry struct {
	key string
	val any
}

func newLRU(size int) *lru {
	return &lru{size: size, order: list.New(), items: map[string]*list.Element{}}
}

func (c *lru) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		return el.Value.(*lruEntry).val, true
	}
	return nil, false
}

func (c *lru) put(key string, val any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*lruEntry).val = val
		c.order.MoveToFront(el)
		return
	}
	c.items[key] = c.order.PushFront(&lruEntry{key, val})
	for c.order.Len() > c.size {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.items, last.Value.(*lruEntry).key)
	}
}

// SourceFromHTML checks a template's stored HTML content the way PageLove
// does (live 2026-09-29, LO-15): an output or tag must open and close
// within one run of text, so one that contains markup ("{{ "<a>x</a>" }}",
// whose "<a>" the HTML tokenizer made an element) is unterminated, a syntax
// error (liquid-filters-string-escaping-family.json in
// harness/observations/live-2026-09-29-serialize). Character references
// inside Liquid markup are decoded later, by the preprocessor, for any
// named HTML host (R-LIQ-13). The content of a raw-text host (script,
// style, …) and XML are returned as is.
func SourceFromHTML(src string, host Host) (string, error) {
	if host.XML || RawTextHost(host.Tag) {
		return src, nil
	}
	for _, m := range templateToken.FindAllStringIndex(src, -1) {
		if strings.IndexByte(src[m[0]:m[1]], '<') < 0 {
			continue
		}
		what := "`{{` interpolation marker"
		if src[m[0]+1] == '%' {
			what = "`{%` tag marker"
		}
		return "", &Error{Kind: KindTemplate, Line: 1 + strings.Count(src[:m[0]], "\n"), Message: "unterminated " + what}
	}
	return src, nil
}

// RawTextHost reports whether a template host's content is raw text stored
// as written (script, style, …).
func RawTextHost(tag string) bool {
	switch tag {
	case "script", "style", "xmp", "iframe", "noembed", "noframes", "plaintext":
		return true
	}
	return false
}
