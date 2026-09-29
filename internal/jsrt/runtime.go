package jsrt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/html"
)

// Options configure a Runtime. Zero fields take the defaults noted.
type Options struct {
	// Size is the number of worker processes (default GOMAXPROCS).
	Size int
	// Executable is the binary started in worker mode (default
	// os.Executable()); it must call MaybeRunWorker (or link this package,
	// whose init does).
	Executable string
	// Host serves callbacks for requests that carry no Host.
	Host Host
	// Grace is added to an evaluation's time budget before the parent
	// SIGKILLs a worker that has not answered (default 250 ms).
	Grace time.Duration
	// RSSLimit is the resident-set ceiling of a worker; a worker above it is
	// killed and the evaluation fails with out-of-memory (default 512 MiB).
	RSSLimit int64
	// RecycleRSS retires a worker after a call whose peak RSS passed it
	// (default 256 MiB).
	RecycleRSS int64
	// MaxCalls retires a worker after this many evaluations (default 5000).
	MaxCalls int
	// StackSlots is the engine's stack limit in libc stack slots, about one
	// JavaScript frame each (default 2000; see DefaultStackSlots).
	StackSlots int
	// AddressSpace is the worker's RLIMIT_AS on Linux (default 4 GiB).
	AddressSpace int64
	// ParallelPrepare makes each worker prepare its next fresh context on a
	// second core while it serves a call, instead of right after replying:
	// higher throughput for back-to-back calls on one worker, at the cost
	// of more CPU per worker and, measured on macOS, colder evaluations
	// after idle periods. Off by default.
	ParallelPrepare bool
	// StartTimeout bounds worker start-up (default 10 s).
	StartTimeout time.Duration
	// Logger receives worker lifecycle events (default: discard).
	Logger *slog.Logger
}

// DefaultStackSlots is the engine stack limit. Measured on the ccgo QuickJS
// port: 2000 slots stop JavaScript recursion at about 1,990 frames, and every
// native recursion path (Array.join, JSON.stringify/parse, RegExp and parser
// nesting, Proxy chains, toString loops, generators) with a catchable
// InternalError/SyntaxError "stack overflow" while the Go stack stays under
// about 16 MiB. The spec requires at least 500 frames (R-JS-56).
const DefaultStackSlots = 2000

func (o *Options) fill() {
	if o.Size <= 0 {
		o.Size = runtime.GOMAXPROCS(0)
	}
	if o.Grace <= 0 {
		o.Grace = 250 * time.Millisecond
	}
	if o.RSSLimit <= 0 {
		o.RSSLimit = 512 << 20
	}
	if o.RecycleRSS <= 0 {
		o.RecycleRSS = 256 << 20
	}
	if o.MaxCalls <= 0 {
		o.MaxCalls = 5000
	}
	if o.StackSlots <= 0 {
		o.StackSlots = DefaultStackSlots
	}
	if o.AddressSpace <= 0 {
		o.AddressSpace = 4 << 30
	}
	if o.StartTimeout <= 0 {
		o.StartTimeout = 10 * time.Second
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
}

// Runtime evaluates server JavaScript in a pool of bounded worker
// processes. It is safe for concurrent use.
type Runtime struct {
	opts     Options
	slots    chan *worker // one token per pool slot; nil = not started
	overflow atomic.Int32
	closed   atomic.Bool

	mu   sync.Mutex
	live map[*worker]bool
}

// New returns a Runtime. Workers start lazily, on first use.
func New(opts Options) *Runtime {
	opts.fill()
	rt := &Runtime{opts: opts, slots: make(chan *worker, opts.Size), live: map[*worker]bool{}}
	for i := 0; i < opts.Size; i++ {
		rt.slots <- nil
	}
	return rt
}

var (
	defaultOnce sync.Once
	defaultRT   *Runtime
)

// Default returns a process-wide Runtime with default Options.
func Default() *Runtime {
	defaultOnce.Do(func() { defaultRT = New(Options{}) })
	return defaultRT
}

// Warm starts every worker of the pool now.
func (rt *Runtime) Warm(ctx context.Context) error {
	var ws []*worker
	defer func() {
		for _, w := range ws {
			rt.release(w, true)
		}
	}()
	for i := 0; i < rt.opts.Size; i++ {
		w, err := rt.acquire(ctx, false)
		if err != nil {
			return err
		}
		ws = append(ws, w)
	}
	return nil
}

// Close stops every worker. Calls in flight fail.
func (rt *Runtime) Close() error {
	if rt.closed.Swap(true) {
		return nil
	}
	rt.mu.Lock()
	ws := make([]*worker, 0, len(rt.live))
	for w := range rt.live {
		ws = append(ws, w)
	}
	rt.mu.Unlock()
	for _, w := range ws {
		w.stop()
	}
	return nil
}

type ctxKey int

const inCallbackKey ctxKey = 1

// acquire takes a worker from the pool, starting one when the slot is
// empty. A call made from inside a Host callback (a JavaScript method that
// reaches JavaScript again through Sessel) must not wait for a slot held by
// its own caller, so it takes an overflow worker when none is idle.
func (rt *Runtime) acquire(ctx context.Context, nested bool) (*worker, error) {
	if rt.closed.Load() {
		return nil, errors.New("jsrt: runtime closed")
	}
	var w *worker
	if nested {
		select {
		case w = <-rt.slots:
		default:
			if rt.overflow.Add(1) > int32(4*rt.opts.Size+4) {
				rt.overflow.Add(-1)
				return nil, errors.New("jsrt: too many nested evaluations")
			}
			nw, err := rt.spawn()
			if err != nil {
				rt.overflow.Add(-1)
				return nil, err
			}
			nw.overflow = true
			return nw, nil
		}
	} else {
		select {
		case w = <-rt.slots:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if w != nil && !w.dead() {
		return w, nil
	}
	if w != nil { // died while idle
		w.stop()
		rt.forget(w)
	}
	nw, err := rt.spawn()
	if err != nil {
		rt.slots <- nil
		return nil, err
	}
	return nw, nil
}

// release returns a worker to its slot, retiring it when it is dead or due
// for recycling.
func (rt *Runtime) release(w *worker, healthy bool) {
	retire := !healthy || w.dead() || w.calls >= rt.opts.MaxCalls || w.peakRSS >= rt.opts.RecycleRSS || rt.closed.Load()
	if w.overflow {
		rt.overflow.Add(-1)
		w.stop()
		rt.forget(w)
		return
	}
	if !retire {
		rt.slots <- w
		return
	}
	w.stop()
	rt.forget(w)
	if rt.closed.Load() {
		rt.slots <- nil
		return
	}
	// Respawn in the background so the next call does not pay for start-up.
	go func() {
		nw, err := rt.spawn()
		if err != nil {
			rt.opts.Logger.Warn("jsrt: respawn failed", "err", err)
			rt.slots <- nil
			return
		}
		if rt.closed.Load() {
			nw.stop()
			rt.forget(nw)
			nw = nil
		}
		rt.slots <- nw
	}()
}

func (rt *Runtime) forget(w *worker) {
	rt.mu.Lock()
	delete(rt.live, w)
	rt.mu.Unlock()
}

// CallModule evaluates a module binding: fresh context, module evaluated,
// default export called per the slot's convention, promises settled, result
// marshalled (docs/spec/javascript.md A2, A3, A6).
func (rt *Runtime) CallModule(ctx context.Context, req CallRequest) (*Result, *Failure) {
	spec, err := lookupSlot(req.Slot)
	if err != nil {
		return nil, &Failure{Variant: VariantInternal, Message: err.Error()}
	}
	if req.Slot == SlotExpression {
		return nil, &Failure{Variant: VariantInternal, Message: "use EvalExpression for j: bindings"}
	}
	if spec.args >= 0 && len(req.Args) != spec.args {
		return nil, &Failure{Variant: VariantInternal, Message: fmt.Sprintf("slot %s takes %d argument(s), got %d", req.Slot, spec.args, len(req.Args))}
	}
	msg := &callMsg{Source: req.Source, Kind: spec.kind, ArgCount: spec.args, DocMode: spec.doc, HasContext: spec.context, HTTPResp: spec.httpResponse, Schemas: req.Schemas}
	b := newBuilder(req.Document)
	if f := b.doc(msg, spec); f != nil {
		return nil, f
	}
	if req.This != nil {
		if _, isUndef := req.This.(Undefined); !isUndef {
			s, f := b.value(req.This)
			if f != nil {
				return nil, f
			}
			msg.This = s
		}
	}
	args, f := b.value(listOf(req.Args))
	if f != nil {
		return nil, f
	}
	msg.Args = args
	if spec.context {
		c := req.Context
		if c == nil {
			c = NewDict()
		}
		s, f := b.value(c)
		if f != nil {
			return nil, f
		}
		msg.Context = s
	}
	if f := b.finish(msg); f != nil {
		return nil, f
	}
	host := req.Host
	if host == nil {
		host = rt.opts.Host
	}
	return rt.run(ctx, msg, req.Budget, host, req.Document, req.Source)
}

// EvalExpression evaluates a j: expression binding: the expression is the
// body of one async strict-mode function whose parameters are the scope's
// identifier names; `this` is undefined; the settled value is marshalled
// (R-JS-31..36).
func (rt *Runtime) EvalExpression(ctx context.Context, req ExprRequest) (*Result, *Failure) {
	spec := slotTable[SlotExpression]
	msg := &callMsg{Expr: true, Source: req.Expression, Kind: spec.kind, ArgCount: -1, DocMode: spec.doc, HasContext: true, Schemas: req.Schemas}
	b := newBuilder(req.Document)
	if f := b.doc(msg, spec); f != nil {
		return nil, f
	}
	scope := req.Scope
	if scope == nil {
		scope = NewDict()
	}
	vals := make([]Value, 0, scope.Len())
	for _, k := range scope.keys {
		msg.Scope = append(msg.Scope, k)
		vals = append(vals, scope.m[k])
	}
	args, f := b.value(vals)
	if f != nil {
		return nil, f
	}
	msg.Args = args
	if req.Request != nil {
		s, f := b.value(req.Request)
		if f != nil {
			return nil, f
		}
		msg.Request = s
	}
	if f := b.finish(msg); f != nil {
		return nil, f
	}
	host := req.Host
	if host == nil {
		host = rt.opts.Host
	}
	return rt.run(ctx, msg, req.Budget, host, req.Document, req.Expression)
}

func listOf(args []Value) []Value {
	if args == nil {
		return []Value{}
	}
	return args
}

// builder marshals one request's values and document.
type builder struct {
	d      *Document
	enc    valueEncoder
	root   *html.Node // Document.Node
	marks  map[*html.Node]int
	nmarks int
}

func newBuilder(d *Document) *builder {
	b := &builder{d: d}
	b.enc.schemas = map[string]bool{}
	if d != nil && d.Node != nil {
		b.root = d.Node
		b.enc.refs = b.ref
	}
	return b
}

// ref resolves an element value that is a node of the request's document:
// 1 is the root of an element-rooted view, other nodes get marker ids.
func (b *builder) ref(n *html.Node) (int, bool) {
	if n == b.root && n.Type == html.ElementNode {
		return 1, true
	}
	for x := n; x != nil; x = x.Parent {
		if x == b.root {
			if b.marks == nil {
				b.marks = map[*html.Node]int{}
			}
			if id, ok := b.marks[n]; ok {
				return id, true
			}
			b.nmarks++
			id := b.nmarks + 1
			b.marks[n] = id
			return id, true
		}
	}
	return 0, false
}

func (b *builder) value(v Value) (string, *Failure) {
	start := b.enc.buf.Len()
	if err := b.enc.encode(v, 0); err != nil {
		return "", &Failure{Variant: VariantMarshal, Message: err.Error()}
	}
	s := b.enc.buf.String()[start:]
	return s, nil
}

func (b *builder) doc(msg *callMsg, spec slotSpec) *Failure {
	if b.d == nil {
		msg.DocMode = docNone
		return nil
	}
	if spec.doc == docNone {
		return &Failure{Variant: VariantInternal, Message: "this slot has no document"}
	}
	msg.Doc = &wireDoc{Source: b.d.Source, XML: b.d.XML}
	if b.d.Node == nil {
		msg.Doc.HTML = b.d.HTML
		return nil
	}
	switch b.d.Node.Type {
	case html.DocumentNode:
	case html.ElementNode:
		msg.Doc.Root = true
		if p := b.d.Node.Parent; p != nil && p.Type == html.ElementNode {
			msg.Doc.Context = p.Data
		}
		msg.Doc.NS = inheritedNamespaces(b.d.Node)
	default:
		return &Failure{Variant: VariantInternal, Message: "Document.Node must be a document or an element"}
	}
	msg.Doc.XML = isXMLNode(b.d.Node)
	return nil
}

// finish serializes the document (after values, which may have added
// markers) and collects the element table.
func (b *builder) finish(msg *callMsg) *Failure {
	msg.Elements = b.enc.elems
	for t := range b.enc.schemas {
		msg.ValueTypes = append(msg.ValueTypes, t)
	}
	if b.d == nil || b.d.Node == nil {
		return nil
	}
	n := b.d.Node
	if len(b.marks) > 0 {
		n = cloneMarked(n, b.marks)
		msg.Doc.Marked = true
	}
	if n.Type == html.DocumentNode {
		msg.Doc.HTML = renderDocument(n)
	} else {
		msg.Doc.HTML = outerHTML(n)
	}
	return nil
}

// inheritedNamespaces collects the xmlns:<prefix> declarations of n's
// ancestors (nearest wins), so a prefixed element keeps its namespaceURI
// in a view rooted at it.
func inheritedNamespaces(n *html.Node) map[string]string {
	var out map[string]string
	for e := n.Parent; e != nil; e = e.Parent {
		if e.Type != html.ElementNode {
			continue
		}
		for _, a := range e.Attr {
			q := a.Key
			if a.Namespace == "xmlns" {
				q = "xmlns:" + a.Key
			}
			if p, ok := strings.CutPrefix(q, "xmlns:"); ok {
				if out == nil {
					out = map[string]string{}
				}
				if _, seen := out[p]; !seen {
					out[p] = a.Val
				}
			}
		}
	}
	return out
}

// refAttr marks nodes of the shipped document that element values refer to;
// the worker removes the attributes right after parsing.
const refAttr = "data-pagelike-jsref"

func cloneMarked(n *html.Node, marks map[*html.Node]int) *html.Node {
	c := &html.Node{Type: n.Type, DataAtom: n.DataAtom, Data: n.Data, Namespace: n.Namespace}
	c.Attr = append([]html.Attribute(nil), n.Attr...)
	if id, ok := marks[n]; ok {
		c.Attr = append(c.Attr, html.Attribute{Key: refAttr, Val: fmt.Sprint(id)})
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.AppendChild(cloneMarked(ch, marks))
	}
	return c
}

func sourceHash(src string) string {
	h := sha256.Sum256([]byte(src))
	return hex.EncodeToString(h[:6])
}

// run executes one call on a worker and converts the outcome.
func (rt *Runtime) run(ctx context.Context, msg *callMsg, budget Budget, host Host, doc *Document, src string) (*Result, *Failure) {
	stats := Stats{Kind: msg.Kind, SourceHash: sourceHash(src), SourceLen: len(src)}
	t0 := time.Now()
	timeout := budget.Time
	if timeout <= 0 {
		timeout = DefaultTime
	}
	if dl, ok := ctx.Deadline(); ok {
		if rem := time.Until(dl); rem < timeout {
			timeout = rem
		}
	}
	if timeout <= 0 {
		stats.Outcome = string(VariantTimeout)
		return nil, &Failure{Variant: VariantTimeout, Message: "time budget exhausted before the evaluation started", Stats: stats}
	}
	msg.TimeoutNS = int64(timeout)
	msg.Memory = budget.Memory
	if msg.Memory <= 0 {
		msg.Memory = DefaultMemory
	}
	msg.DOMOps = budget.DOMOps

	nested := ctx.Value(inCallbackKey) != nil
	w, err := rt.acquire(ctx, nested)
	if err != nil {
		stats.Outcome = string(VariantInternal)
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, &Failure{Variant: VariantTimeout, Message: "time budget exhausted waiting for a worker", Stats: stats}
		}
		return nil, &Failure{Variant: VariantInternal, Message: "no JavaScript worker: " + err.Error(), Stats: stats}
	}
	res, fail := w.call(ctx, msg, timeout, rt.opts, host)
	rt.release(w, fail == nil || fail.Variant != VariantInternal && !w.dead())
	stats.RoundTrip = time.Since(t0)
	stats.WorkerPID = w.pid
	if res != nil {
		stats.Elapsed = time.Duration(res.ElapsedNS)
		stats.MemoryUsed = res.MemoryUsed
		stats.DOMOps = res.DOMOps
		stats.HostCalls = res.HostCalls
		stats.WorkerRSS = res.RSS
	}
	if fail != nil {
		stats.Outcome = string(fail.Variant)
		fail.Stats = stats
		return nil, fail
	}
	return convertResult(res, msg, stats)
}

func convertResult(res *resultMsg, msg *callMsg, stats Stats) (*Result, *Failure) {
	stats.Outcome = res.Outcome
	if res.Outcome != "ok" {
		f := &Failure{Variant: Variant(res.Outcome), Message: res.Message, Stack: res.Stack, Specifier: res.Specifier, Stats: stats}
		if res.Response != nil && msg.HTTPResp {
			f.Response = &HTTPResponse{Status: res.Response.Status, Message: res.Response.Message, Body: res.Response.Body, Headers: cleanHeaders(res.Response.Headers)}
		}
		return nil, f
	}
	v, err := decodeValue([]byte(res.Value), res.Elements)
	if err != nil {
		stats.Outcome = string(VariantInternal)
		return nil, &Failure{Variant: VariantInternal, Message: "bad result from worker: " + err.Error(), Stats: stats}
	}
	out := &Result{Value: v, Document: res.Document, DocumentChanged: res.DocumentChanged, Tainted: res.Tainted, Stats: stats}
	for _, cw := range res.ContextWrites {
		w := ContextWrite{Name: cw.Name, Deleted: cw.Deleted}
		if !cw.Deleted {
			if w.Value, err = decodeValue([]byte(cw.Value), cw.Elements); err != nil {
				return nil, &Failure{Variant: VariantInternal, Message: "bad context write from worker: " + err.Error(), Stats: stats}
			}
		}
		out.ContextWrites = append(out.ContextWrites, w)
	}
	return out, nil
}

// cleanHeaders drops header names that are not RFC 9110 tokens and values
// with control characters (R-JS-53).
func cleanHeaders(hs [][2]string) [][2]string {
	var out [][2]string
	for _, h := range hs {
		if !isToken(h[0]) || strings.ContainsFunc(h[1], func(r rune) bool { return r < 0x20 && r != '\t' || r == 0x7f }) {
			continue
		}
		out = append(out, h)
	}
	return out
}

func isToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// Executable reports the binary workers run.
func (rt *Runtime) executable() (string, error) {
	if rt.opts.Executable != "" {
		return rt.opts.Executable, nil
	}
	return os.Executable()
}
