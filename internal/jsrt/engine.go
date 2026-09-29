package jsrt

import (
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/net/html"
	"modernc.org/libc"
	lib "modernc.org/libquickjs"
	"modernc.org/quickjs"

	"github.com/sky-valley/pagelike/internal/dom"
)

//go:embed prelude.js
var preludeJS string

//go:embed prelude_dom.js
var preludeDOMJS string

// preludeSource is the core prelude with the interface names and the
// DOMException codes filled in; preludeDOMSource the DOM chunk with the
// interface table.
var preludeSource, preludeDOMSource = func() (string, string) {
	ex, _ := json.Marshal(domExceptionCodes)
	names := make([]string, len(ifaceTable))
	for i, d := range ifaceTable {
		names[i] = d.Name
	}
	nj, _ := json.Marshal(names)
	core := strings.Replace(preludeJS, "/*IFACENAMES*/[]", string(nj), 1)
	core = strings.Replace(core, "/*DOMEX*/[]", string(ex), 1)
	return core, strings.Replace(preludeDOMJS, "/*IFACES*/[]", ifaceJSON, 1)
}()

// engine evaluates calls inside a worker process. Every evaluation gets a
// fresh QuickJS runtime and context (R-JS-5); the prelude is compiled once
// per process and loaded as bytecode. Contexts are prepared ahead on one
// goroutine and freed on another, in parallel with the evaluation being
// served (QuickJS runtimes are independent), so a call only pays for its
// own evaluation.
type engine struct {
	cfg workerConfig

	codeMu   sync.Mutex
	bytecode []byte
	domCode  []byte

	baseline atomic.Int64 // heap bytes of a fresh context with the prelude
	wantDOM  atomic.Bool  // the last evaluation used the DOM: preload it
	spares   chan prepared
	spare    *evalState // serial mode: prepared after the last reply
	closing  chan *evalState
	eval     *meter // used on the evaluating goroutine only
}

type prepared struct {
	st  *evalState
	err error
}

// meter measures a runtime's heap. It owns a libc TLS, so each goroutine
// that measures has its own.
type meter struct {
	tls   *libc.TLS
	usage uintptr // C buffer for JSMemoryUsage
}

func newMeter() *meter {
	m := &meter{tls: libc.NewTLS()}
	m.usage = libc.Xcalloc(m.tls, 1, libc.Tsize_t(unsafe.Sizeof(lib.TJSMemoryUsage{})))
	return m
}

func newEngine(cfg workerConfig) *engine {
	return &engine{cfg: cfg, spares: make(chan prepared), closing: make(chan *evalState, 8), eval: newMeter()}
}

// start runs the preparing and freeing goroutines.
func (e *engine) start() {
	if e.cfg.parallelPrepare {
		go func() {
			m := newMeter()
			for {
				st, err := e.prepare(m)
				e.spares <- prepared{st, err}
			}
		}()
	}
	go func() {
		for st := range e.closing {
			func() {
				defer func() { _ = recover() }()
				if !st.panicked {
					st.vm.Close()
				}
			}()
		}
	}()
}

// vmContext reads the JSContext pointer, the first field of quickjs.VM
// (modernc.org/quickjs v0.24.2), to reach JS_ComputeMemoryUsage and
// JS_SetStripInfo, which the wrapper does not expose (ADR 0001 Risk 4).
func vmContext(vm *quickjs.VM) uintptr { return *(*uintptr)(unsafe.Pointer(vm)) }

// heapSize is the runtime's current malloc size.
func (m *meter) heapSize(vm *quickjs.VM) int64 {
	ctx := vmContext(vm)
	if ctx == 0 {
		return 0
	}
	rt := lib.XJS_GetRuntime(m.tls, ctx)
	if rt == 0 {
		return 0
	}
	lib.XJS_ComputeMemoryUsage(m.tls, rt, m.usage)
	// malloc_size is the first field of JSMemoryUsage.
	b := libc.GoBytes(m.usage, 8)
	return *(*int64)(unsafe.Pointer(&b[0]))
}

type modKind int

const (
	modUser modKind = iota
	modMethod
	modSchema
	modEntry
)

type modInfo struct {
	kind    modKind
	source  string
	allowed map[string]bool
	scan    *prescanResult
}

// evalState is one evaluation.
type evalState struct {
	e        *engine
	call     *callMsg
	conn     *conn
	vm       *quickjs.VM
	deadline time.Time
	dom      *domState

	mods       map[string]*modInfo
	entry      string
	entryTaken bool
	running    bool
	parsed     bool
	importFail *resultMsg

	schemas map[string]*SchemaDescriptor
	unknown map[string]bool

	cbSeq     int
	hostCalls int
	ctxWrites []wireContextWrite
	tainted   bool
	sysErr    string
	hostErr   error
	panicked  bool
	domLoaded bool
	// meter belongs to the goroutine driving this context right now (the
	// preparing one, then the evaluating one): libc TLS is not shared.
	meter *meter
}

// prepare builds a fresh runtime and context with the prelude loaded, ready
// for exactly one evaluation.
func (e *engine) prepare(m *meter) (st *evalState, err error) {
	defer func() {
		if r := recover(); r != nil {
			st, err = nil, fmt.Errorf("prepare: %v", r)
		}
	}()
	st = &evalState{e: e, mods: map[string]*modInfo{}, schemas: map[string]*SchemaDescriptor{}, unknown: map[string]bool{}, meter: m}
	vm, err := quickjs.NewVM()
	if err != nil {
		return nil, fmt.Errorf("new VM: %w", err)
	}
	st.vm = vm
	vm.SetMaxStackSize(uintptr(e.cfg.stackSlots))
	// Reference counting frees most garbage at once; the cycle collector
	// only needs to run when the heap grows, not while the prelude builds.
	vm.SetGCThreshold(4 << 20)
	st.dom = newDOMState(0, 0)
	st.dom.onOpsExceeded = vm.Interrupt
	if err := vm.RegisterHostFunc("__pl_dom", st.domHost); err != nil {
		vm.Close()
		return nil, err
	}
	if err := vm.RegisterHostFunc("__pl_sys", st.sysHost); err != nil {
		vm.Close()
		return nil, err
	}
	bc, err := e.code(m, &e.bytecode, preludeSource)
	if err == nil {
		_, err = vm.EvalBytecode(bc)
	}
	if err != nil {
		vm.Close()
		return nil, fmt.Errorf("prelude: %w", err)
	}
	if e.baseline.Load() == 0 {
		e.baseline.Store(m.heapSize(vm))
	}
	if e.wantDOM.Load() {
		if _, err := vm.Call("__pagelike.preloadDOM"); err != nil {
			vm.Close()
			return nil, fmt.Errorf("DOM preload: %w", err)
		}
	}
	return st, nil
}

// prepareNext readies the next context on the calling goroutine when
// contexts are not prepared in parallel.
func (e *engine) prepareNext() {
	if !e.cfg.parallelPrepare && e.spare == nil {
		e.spare, _ = e.prepare(e.eval)
	}
}

// evaluate serves one call on the next prepared context and hands the
// context to the freeing goroutine afterwards.
func (e *engine) evaluate(call *callMsg, c *conn) (res *resultMsg) {
	var p prepared
	if e.cfg.parallelPrepare {
		p = <-e.spares
	} else if e.spare != nil {
		p, e.spare = prepared{st: e.spare}, nil
	} else {
		p.st, p.err = e.prepare(e.eval)
	}
	t0 := time.Now()
	if p.err != nil {
		return &resultMsg{Outcome: string(VariantInternal), Message: p.err.Error()}
	}
	st := p.st
	st.meter = e.eval
	st.call, st.conn, st.deadline = call, c, t0.Add(time.Duration(call.TimeoutNS))
	st.dom.maxAlloc, st.dom.maxOps = call.Memory, call.DOMOps
	defer func() {
		if r := recover(); r != nil {
			// A panic may have unwound through the engine; never touch this
			// context again.
			st.panicked = true
			res = &resultMsg{Outcome: string(VariantInternal), Message: fmt.Sprintf("worker panic: %v", r)}
		}
		res.ElapsedNS = int64(time.Since(t0))
		res.DOMOps = st.dom.ops
		res.HostCalls = st.hostCalls
		if res.Outcome == "ok" && !st.panicked {
			res.MemoryUsed = max(0, e.eval.heapSize(st.vm)-e.baseline.Load())
		}
		e.wantDOM.Store(st.domLoaded)
		e.closing <- st
	}()
	return st.run()
}

func fail(v Variant, format string, a ...any) *resultMsg {
	return &resultMsg{Outcome: string(v), Message: fmt.Sprintf(format, a...)}
}

func (st *evalState) run() *resultMsg {
	call := st.call
	vm := st.vm
	vm.SetMemoryLimit(uintptr(st.e.baseline.Load() + call.Memory))
	vm.SetModuleLoader(st.load, st.normalize)

	// The binding's module.
	src := call.Source
	var params []int
	if call.Expr {
		var f *resultMsg
		src, params, f = st.exprModule()
		if f != nil {
			return f
		}
	}
	scan := prescan(src)
	st.addUserModule("eval", modUser, src, scan)

	// Schemas: the binding's imports, the types of instance/class inputs,
	// parents and the imports of JavaScript method bodies.
	roots := append(scan.schemaImports(), call.ValueTypes...)
	if err := st.resolveSchemas(roots); err != nil {
		return fail(VariantInternal, "schema lookup: %v", err)
	}
	var valueTypes []string
	for _, t := range call.ValueTypes {
		if st.schemas[t] != nil {
			valueTypes = append(valueTypes, t)
		}
	}
	st.entry = "pl:entry#" + nonce()
	entryAllowed := map[string]bool{"eval": true}
	for _, t := range valueTypes {
		entryAllowed[t] = true
	}
	st.mods[st.entry] = &modInfo{kind: modEntry, source: entryModuleSource(valueTypes), allowed: entryAllowed}

	// The DOM: the ambient document and element values.
	handles, docHandle, f := st.loadDOM()
	if f != nil {
		return f
	}
	cfg := map[string]any{
		"entry": st.entry, "doc": docHandle, "handles": handles, "hasContext": call.HasContext,
		"expr": call.Expr, "scope": call.Scope, "this": call.This, "args": call.Args, "interfaces": st.needsInterfaces(),
		"context": call.Context, "request": call.Request,
	}
	if call.Expr {
		cfg["params"] = params
	}
	cj, _ := json.Marshal(cfg)
	if !st.arm() {
		return st.timeout()
	}
	if _, err := st.driverCall(0, "__pagelike.setup", string(cj)); err != nil {
		return st.engineFailure(err)
	}
	if !st.arm() {
		return st.timeout()
	}
	if _, err := st.driverCall(0, "__pagelike.start"); err != nil {
		return st.engineFailure(err)
	}
	// Drive the job queue until the binding's promise settles (R-JS-8).
	for {
		if !st.arm() {
			return st.timeout()
		}
		n, err := vm.ExecutePendingJobs()
		if err != nil {
			return st.engineFailure(err)
		}
		s, err := st.driverCall(0, "__pagelike.state")
		if err != nil {
			return st.engineFailure(err)
		}
		if s == "done" {
			break
		}
		if n == 0 {
			return &resultMsg{Outcome: string(VariantThrew), Message: "Error: the binding's promise never settled (there are no timers or I/O to settle it)"}
		}
	}
	// Marshal the result with headroom, so a result that fills the budget
	// can still be encoded.
	if !st.arm() {
		return st.timeout()
	}
	out, err := st.driverCall(call.Memory, "__pagelike.finish")
	if err != nil {
		return st.engineFailure(err)
	}
	s, _ := out.(string)
	return st.outcome(s)
}

// driverHeadroom is heap the driver may use beyond the binding's budget, so
// it can still report on a binding that exhausted it.
const driverHeadroom = 4 << 20

// driverCall calls a driver function with extra heap headroom.
func (st *evalState) driverCall(extra int64, fn string, args ...any) (any, error) {
	limit := st.e.baseline.Load() + st.call.Memory
	st.vm.SetMemoryLimit(uintptr(limit + driverHeadroom + extra))
	defer st.vm.SetMemoryLimit(uintptr(limit))
	return st.vm.Call(fn, args...)
}

// arm sets the engine deadline to the time left; false when none is.
func (st *evalState) arm() bool {
	if st.dom != nil && st.dom.opsExceeded {
		return false
	}
	rem := time.Until(st.deadline)
	if rem <= 0 {
		return false
	}
	st.vm.SetEvalTimeout(rem)
	return true
}

func (st *evalState) timeout() *resultMsg {
	if st.dom != nil && st.dom.opsExceeded {
		return fail(VariantTimeout, "DOM operation budget of %d exhausted", st.call.DOMOps)
	}
	return fail(VariantTimeout, "time budget of %v exhausted", time.Duration(st.call.TimeoutNS))
}

func (st *evalState) oom() *resultMsg {
	return fail(VariantOutOfMemory, "memory budget of %d bytes exhausted", st.call.Memory)
}

// engineFailure classifies an error that escaped the driver: an uncatchable
// interrupt, or a failure in the driver itself.
func (st *evalState) engineFailure(err error) *resultMsg {
	if st.dom.opsExceeded || !time.Now().Before(st.deadline) {
		return st.timeout()
	}
	if st.dom.oom || st.meter.heapSize(st.vm) >= st.e.baseline.Load()+st.call.Memory-(64<<10) {
		return st.oom()
	}
	var qe *quickjs.Error
	if errors.As(err, &qe) {
		switch {
		case qe.Message == "interrupted":
			return st.timeout()
		case qe.Message == "out of memory", qe.Error() == "null", st.dom.oom:
			return st.oom()
		case strings.Contains(qe.Message, "stack overflow"):
			return &resultMsg{Outcome: string(VariantThrew), Message: qe.Name + ": " + qe.Message}
		}
	}
	if st.hostErr != nil {
		return fail(VariantInternal, "host callback: %v", st.hostErr)
	}
	return fail(VariantInternal, "engine: %v", err)
}

// driverOutcome is what __pagelike.finish reports.
type driverOutcome struct {
	Outcome string          `json:"outcome"`
	Value   json.RawMessage `json:"value"`
	Elems   []int           `json:"elems"`
	Message string          `json:"message"`
	IsNull  bool            `json:"isNull"`
	Name    string          `json:"name"`
	Stack   string          `json:"stack"`
	Text    string          `json:"text"`
	HTTP    *wireResponse   `json:"http"`
}

var throwNullRE = regexp.MustCompile(`\bthrow\s+null\b|reject\(\s*null\s*\)`)

func (st *evalState) outcome(s string) *resultMsg {
	var o driverOutcome
	if err := json.Unmarshal([]byte(s), &o); err != nil {
		return fail(VariantInternal, "bad driver output: %v", err)
	}
	switch o.Outcome {
	case "ok":
		res := &resultMsg{Outcome: "ok", Value: string(o.Value), Tainted: st.tainted, ContextWrites: st.ctxWrites}
		for _, h := range o.Elems {
			if h < 0 || h >= len(st.dom.nodes) {
				return fail(VariantInternal, "bad element handle in result")
			}
			n := st.dom.nodes[h]
			res.Elements = append(res.Elements, resultElem{HTML: outerHTML(n), Source: st.dom.sourceOf(n)})
		}
		if st.call.DocMode == docWritable && st.dom.ambientDirty && st.dom.ambient != nil {
			res.DocumentChanged = true
			if st.call.Doc != nil && st.call.Doc.Root {
				if r := dom.DocumentElement(st.dom.ambient); r != nil {
					res.Document = outerHTML(r)
				}
			} else {
				res.Document = renderDocument(st.dom.ambient)
			}
		}
		return res
	case "shape":
		return fail(VariantShape, "%s", o.Message)
	case "return-type":
		return fail(VariantReturnType, "%s", o.Message)
	case "pending":
		return fail(VariantInternal, "the evaluation did not finish")
	case "load":
		if st.importFail != nil {
			return st.importFail
		}
		if !st.parsed && o.Name == "SyntaxError" {
			msg := o.Text
			if loc := firstLocation(o.Stack); loc != "" {
				msg += " (" + loc + ")"
			}
			return fail(VariantParse, "%s", msg)
		}
	}
	// A thrown value (from the module body or the binding).
	switch {
	case o.Name == "InternalError" && o.Message == "interrupted", st.dom.opsExceeded:
		return st.timeout()
	case o.Name == "InternalError" && o.Message == "out of memory", st.dom.oom,
		o.IsNull && !throwNullRE.MatchString(st.call.Source):
		// A thrown null after the heap limit is the engine failing to
		// allocate the error object (ADR 0001 Risks, R-JS-51).
		return st.oom()
	case st.importFail != nil && strings.Contains(o.Text, "module normalization failed"):
		return st.importFail
	}
	res := &resultMsg{Outcome: string(VariantThrew), Message: o.Text, Stack: cleanStack(o.Stack)}
	if o.HTTP != nil {
		res.Response = o.HTTP
	}
	return res
}

var locRE = regexp.MustCompile(`\(?(eval:\d+:\d+)\)?`)

func firstLocation(stack string) string {
	if m := locRE.FindStringSubmatch(stack); m != nil {
		return m[1]
	}
	return ""
}

// cleanStack keeps the binding's frames: its module ("eval"), JavaScript
// method bodies ("<type>#<method>") and native frames; prelude, driver and
// generated-module frames are dropped.
func cleanStack(stack string) string {
	var keep []string
	for _, l := range strings.Split(stack, "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		loc := t
		if i := strings.LastIndexByte(t, '('); i >= 0 {
			loc = t[i+1:]
		} else {
			loc = strings.TrimPrefix(t, "at ")
		}
		switch {
		case strings.HasPrefix(loc, "eval:"), strings.HasPrefix(loc, "native"), strings.Contains(loc, "#") && !strings.HasPrefix(loc, "pl:"):
			keep = append(keep, "    "+t)
		}
		if len(keep) >= 32 {
			keep = append(keep, "    ...")
			break
		}
	}
	// Frames below the binding's outermost frame are the driver's.
	for len(keep) > 0 && strings.HasSuffix(keep[len(keep)-1], "(native)") {
		keep = keep[:len(keep)-1]
	}
	return strings.Join(keep, "\n")
}

// ------------------------------------------------------------ modules

func (st *evalState) addUserModule(name string, kind modKind, src string, scan *prescanResult) {
	allowed := map[string]bool{}
	for _, s := range scan.schemaImports() {
		allowed[s] = true
	}
	suffix := "\n;"
	if scan.Problem != "" {
		suffix += "import \"pl:reject\";"
	}
	if name == "eval" {
		suffix += "import \"pl:begin\";"
	}
	st.mods[name] = &modInfo{kind: kind, source: src + suffix + "\n", allowed: allowed, scan: scan}
}

func (st *evalState) rejectImport(v Variant, spec, why string) error {
	if st.importFail == nil {
		st.importFail = &resultMsg{Outcome: string(v), Message: why, Specifier: spec}
		if spec != "" && !strings.Contains(why, spec) {
			st.importFail.Message = why + ": " + spec
		}
	}
	return errors.New(string(v) + ": " + why)
}

func (st *evalState) normalize(_ *quickjs.VM, base, name string) (string, error) {
	if st.running {
		return "", st.rejectImport(VariantImportNotAllowed, name, "dynamic import() is not allowed")
	}
	if name == st.entry && !st.entryTaken {
		st.entryTaken = true
		return name, nil
	}
	bm := st.mods[base]
	if bm == nil {
		return "", st.rejectImport(VariantImportNotAllowed, name, "imports are not allowed here")
	}
	switch bm.kind {
	case modUser, modMethod:
		if name == "pl:begin" && base == "eval" {
			st.parsed = true
			return name, nil
		}
		if name == "pl:reject" {
			return "", st.rejectImport(VariantImportNotAllowed, bm.scan.ProblemSpec, bm.scan.Problem)
		}
		if bm.allowed[name] {
			if st.unknown[name] || st.schemas[name] == nil {
				return "", st.rejectImport(VariantUnknownSchema, name, "no schema is registered for")
			}
			return name, nil
		}
		return "", st.rejectImport(VariantImportNotAllowed, name, "only schema imports are allowed")
	case modSchema, modEntry:
		if bm.allowed[name] {
			return name, nil
		}
	}
	return "", st.rejectImport(VariantImportNotAllowed, name, "import not allowed")
}

func (st *evalState) load(_ *quickjs.VM, name string) (string, error) {
	if name == "pl:begin" {
		return "", nil
	}
	if m := st.mods[name]; m != nil {
		return m.source, nil
	}
	return "", errors.New("no module " + name)
}

// resolveSchemas finds descriptors for the schema URLs reachable from
// roots: from the request first, then from the host.
func (st *evalState) resolveSchemas(roots []string) error {
	provided := map[string]*SchemaDescriptor{}
	for _, d := range st.call.Schemas {
		if d != nil {
			provided[d.Type] = d
		}
	}
	queue := append([]string(nil), roots...)
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		if st.schemas[u] != nil || st.unknown[u] {
			continue
		}
		d := provided[u]
		if d == nil {
			r, err := st.callback(&cbMsg{Op: "schema", Type: u})
			if err != nil {
				return err
			}
			if r.Error != "" {
				return errors.New(r.Error)
			}
			d = r.Schema
		}
		if d == nil || d.Type != u {
			st.unknown[u] = true
			continue
		}
		st.schemas[u] = d
		if d.Parent != "" && d.Parent != MapSchemaType {
			queue = append(queue, d.Parent)
		}
		for _, m := range d.Methods {
			if isJSMethod(m) {
				queue = append(queue, prescan(m.Source).schemaImports()...)
			}
		}
	}
	// Generate the class modules and method modules.
	for u, d := range st.schemas {
		parentOK := d.Parent != "" && d.Parent != MapSchemaType && st.schemas[d.Parent] != nil
		allowed := map[string]bool{}
		if parentOK {
			allowed[d.Parent] = true
		}
		for _, m := range d.Methods {
			if isJSMethod(m) {
				name := methodModuleName(d, m)
				allowed[name] = true
				st.addUserModule(name, modMethod, m.Source, prescan(m.Source))
			}
		}
		st.mods[u] = &modInfo{kind: modSchema, source: classModuleSource(d, parentOK, d.Parent == MapSchemaType), allowed: allowed}
	}
	return nil
}

// reachesGlobalsRE finds sources that could reach the per-tag interface
// globals: by name, or through reflection on the global object or code
// built at runtime.
var reachesGlobalsRE = regexp.MustCompile(`HTML|globalThis|eval|Function|constructor|Reflect|getOwnProperty|getPrototypeOf|Proxy|\\u`)

// needsInterfaces reports whether any module of this evaluation could
// observe the per-tag interface globals (HTMLAnchorElement, …). Defining
// them costs a property definition each, so they are only defined when
// needed; elements get their classes either way.
func (st *evalState) needsInterfaces() bool {
	for _, m := range st.mods {
		if (m.kind == modUser || m.kind == modMethod) && reachesGlobalsRE.MatchString(m.source) {
			return true
		}
	}
	return false
}

var identRE = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

var reservedWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`break case catch class const continue debugger default delete do else enum export extends
		false finally for function if import in instanceof new null return super switch this throw true try typeof var void
		while with yield let static implements interface package private protected public await arguments eval`) {
		reservedWords[w] = true
	}
}

// isBareName reports a j: binding name usable as a bare identifier
// (R-JS-32).
func isBareName(s string) bool { return identRE.MatchString(s) && !reservedWords[s] }

// exprModule builds the module for a j: expression: the expression is the
// body of one async strict function over the identifier-named bindings
// (R-JS-31). It must parse both inside parentheses and inside brackets,
// which rejects text that closes the wrapper and continues with statements.
func (st *evalState) exprModule() (string, []int, *resultMsg) {
	var names []string
	params := []int{}
	for i, n := range st.call.Scope {
		if isBareName(n) {
			names = append(names, n)
			params = append(params, i)
		}
	}
	expr := st.call.Source
	head := "(async function (" + strings.Join(names, ", ") + ") { return "
	check := func(open, close string) error {
		v, err := st.vm.EvalValue("\"use strict\";"+head+open+"\n"+expr+"\n"+close+"; })", quickjs.EvalGlobal|lib.MJS_EVAL_FLAG_COMPILE_ONLY)
		if err == nil {
			v.Free()
		}
		return err
	}
	if err := check("(", ")"); err != nil {
		var qe *quickjs.Error
		msg := err.Error()
		if errors.As(err, &qe) {
			msg = qe.Name + ": " + qe.Message
		}
		return "", nil, fail(VariantParse, "%s", msg)
	}
	if err := check("[", "]"); err != nil {
		return "", nil, fail(VariantParse, "SyntaxError: a j: binding must be one JavaScript expression")
	}
	src := "export default async function (" + strings.Join(names, ", ") + ") { return (\n" + expr + "\n); }"
	return src, params, nil
}

// code returns the bytecode for a prelude chunk, compiling it on first use.
func (e *engine) code(m *meter, slot *[]byte, src string) ([]byte, error) {
	e.codeMu.Lock()
	defer e.codeMu.Unlock()
	if *slot == nil {
		bc, err := compileStripped(m, src)
		if err != nil {
			return nil, err
		}
		*slot = bc
	}
	return *slot, nil
}

// compileStripped compiles trusted prelude source in a throwaway runtime
// with the source text stripped (debug info stays: import() needs the file
// name), so the bytecode is a fraction of the size. Bindings keep theirs.
func compileStripped(m *meter, src string) ([]byte, error) {
	vm, err := quickjs.NewVM()
	if err != nil {
		return nil, err
	}
	defer vm.Close()
	if ctx := vmContext(vm); ctx != 0 {
		lib.XJS_SetStripInfo(m.tls, lib.XJS_GetRuntime(m.tls, ctx), lib.MJS_STRIP_SOURCE)
	}
	return vm.Compile(src, quickjs.EvalGlobal)
}

// loadDOM parses the ambient document and the element values.
func (st *evalState) loadDOM() ([]int, int, *resultMsg) {
	call := st.call
	refs := map[int]*html.Node{}
	docHandle := -1
	if call.Doc != nil && call.DocMode != docNone {
		doc, r, err := st.dom.loadDocument(call.Doc, call.DocMode == docReadOnly)
		if err != nil {
			return nil, -1, fail(VariantInternal, "document: %v", err)
		}
		refs = r
		docHandle = st.dom.handle(doc)
	}
	handles := make([]int, len(call.Elements))
	for i, we := range call.Elements {
		if we.Ref > 0 {
			n := refs[we.Ref]
			if n == nil {
				return nil, -1, fail(VariantInternal, "element reference %d not found in the document", we.Ref)
			}
			handles[i] = st.dom.handle(n)
			continue
		}
		n, err := st.dom.loadElement(we)
		if err != nil {
			return nil, -1, fail(VariantMarshal, "element value: %v", err)
		}
		handles[i] = st.dom.handle(n)
	}
	return handles, docHandle, nil
}

func nonce() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ------------------------------------------------------------ host functions

func normArgs(args []any) []any {
	for i, a := range args {
		if _, ok := a.(quickjs.Undefined); ok {
			args[i] = nil
		}
	}
	return args
}

// domHost is __pl_dom. It never panics into the engine.
func (st *evalState) domHost(args []any) (r any, err error) {
	defer func() {
		if p := recover(); p != nil {
			st.dom.errName, st.dom.errMsg = "InternalError", fmt.Sprint(p)
			r, err = errSentinel, nil
		}
	}()
	return st.dom.op(normArgs(args))
}

// sysHost is __pl_sys: callbacks, taint, crypto and text decoding.
func (st *evalState) sysHost(args []any) (r any, err error) {
	defer func() {
		if p := recover(); p != nil {
			st.sysErr = fmt.Sprint(p)
			r, err = errSentinel, nil
		}
	}()
	args = normArgs(args)
	op, _ := arg(args, 0).(string)
	r, e := st.sys(op, args[1:])
	if e != nil {
		st.sysErr = e.Error()
		return errSentinel, nil
	}
	return r, nil
}

func latin1Bytes(s string) []byte {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		b = append(b, byte(r))
	}
	return b
}

func (st *evalState) sys(op string, a []any) (any, error) {
	switch op {
	case "running":
		st.running = true
		return nil, nil
	case "dom":
		// Evaluate the DOM chunk re-entrantly; the engine deadline is re-armed
		// because the evaluation entry resets it.
		bc, err := st.e.code(st.meter, &st.e.domCode, preludeDOMSource)
		if err != nil {
			return nil, err
		}
		if st.deadline.IsZero() {
			st.vm.SetEvalTimeout(0)
		} else if rem := time.Until(st.deadline); rem > 0 {
			st.vm.SetEvalTimeout(rem)
		} else {
			return nil, errors.New("time budget exhausted")
		}
		if _, err := st.vm.EvalBytecode(bc); err != nil {
			return nil, err
		}
		st.domLoaded = true
		return nil, nil
	case "taint":
		st.tainted = true
		return nil, nil
	case "err":
		return st.sysErr, nil
	case "digest":
		var h hash.Hash
		switch toStr(arg(a, 0)) {
		case "SHA-1":
			h = sha1.New()
		case "SHA-256":
			h = sha256.New()
		case "SHA-384":
			h = sha512.New384()
		case "SHA-512":
			h = sha512.New()
		default:
			return nil, errors.New("unsupported digest")
		}
		h.Write(latin1Bytes(toStr(arg(a, 1))))
		return hex.EncodeToString(h.Sum(nil)), nil
	case "random":
		n, _ := toInt(arg(a, 0))
		if n < 0 || n > 65536 {
			return nil, errors.New("bad length")
		}
		b := make([]byte, n)
		_, _ = rand.Read(b)
		return hex.EncodeToString(b), nil
	case "uuid":
		var b [16]byte
		_, _ = rand.Read(b[:])
		b[6] = b[6]&0x0f | 0x40
		b[8] = b[8]&0x3f | 0x80
		x := hex.EncodeToString(b[:])
		return x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:], nil
	case "utf8":
		b := latin1Bytes(toStr(arg(a, 0)))
		fatal, _ := arg(a, 1).(bool)
		if fatal && !utf8.Valid(b) {
			return nil, errors.New("TypeError: the encoded data was not valid utf-8")
		}
		s := strings.ToValidUTF8(string(b), string(utf8.RuneError))
		return strings.TrimPrefix(s, string(rune(0xFEFF))), nil
	case "method":
		elems, err := st.resultElems(toStr(arg(a, 4)))
		if err != nil {
			return nil, err
		}
		static, _ := arg(a, 2).(bool)
		r, err := st.callback(&cbMsg{Op: "method", Type: toStr(arg(a, 0)), Method: toStr(arg(a, 1)), Static: static, Value: toStr(arg(a, 3)), Elements: elems})
		if err != nil {
			return nil, err
		}
		if r.Error != "" {
			return nil, errors.New(r.Error)
		}
		return st.replyJSON(r)
	case "context":
		name := toStr(arg(a, 0))
		deleted, _ := arg(a, 3).(bool)
		elems, err := st.resultElems(toStr(arg(a, 2)))
		if err != nil {
			return nil, err
		}
		w := wireContextWrite{Name: name, Value: toStr(arg(a, 1)), Elements: elems, Deleted: deleted}
		r, err := st.callback(&cbMsg{Op: "context", Name: name, Value: w.Value, Elements: elems, Deleted: deleted})
		if err != nil {
			return nil, err
		}
		if r.Error != "" {
			return nil, errors.New(r.Error)
		}
		st.ctxWrites = append(st.ctxWrites, w)
		return nil, nil
	}
	return nil, fmt.Errorf("unknown sys op %q", op)
}

func (st *evalState) resultElems(handlesJSON string) ([]resultElem, error) {
	var hs []int
	if handlesJSON != "" {
		if err := json.Unmarshal([]byte(handlesJSON), &hs); err != nil {
			return nil, err
		}
	}
	out := make([]resultElem, 0, len(hs))
	for _, h := range hs {
		if h < 0 || h >= len(st.dom.nodes) {
			return nil, errors.New("bad element handle")
		}
		n := st.dom.nodes[h]
		out = append(out, resultElem{HTML: outerHTML(n), Source: st.dom.sourceOf(n)})
	}
	return out, nil
}

// replyJSON turns a method reply into what the prelude decodes: the value
// and Context changes as wire JSON texts, and handles for their elements.
func (st *evalState) replyJSON(r *replyMsg) (string, error) {
	hs := make([]int, len(r.Elements))
	for i, we := range r.Elements {
		n, err := st.dom.loadElement(we)
		if err != nil {
			return "", err
		}
		hs[i] = st.dom.handle(n)
	}
	type ctxOut struct {
		Name    string `json:"name"`
		Value   string `json:"value"`
		Deleted bool   `json:"deleted"`
	}
	out := struct {
		Value   string   `json:"value"`
		Handles []int    `json:"handles"`
		Context []ctxOut `json:"context"`
	}{Value: r.Value, Handles: hs}
	if out.Value == "" {
		out.Value = "null"
	}
	for _, c := range r.Context {
		out.Context = append(out.Context, ctxOut{Name: c.Name, Value: c.Value, Deleted: c.Deleted})
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// callback asks the host and waits for its reply. The JavaScript thread is
// blocked meanwhile; the time counts against the evaluation's budget.
func (st *evalState) callback(cb *cbMsg) (*replyMsg, error) {
	st.hostCalls++
	st.cbSeq++
	cb.ID = st.cbSeq
	if err := st.conn.send(&frame{T: "cb", CB: cb}); err != nil {
		st.hostErr = err
		return nil, err
	}
	f, err := st.conn.recv()
	if err != nil {
		st.hostErr = err
		return nil, err
	}
	if f.T != "reply" || f.Reply == nil || f.Reply.ID != cb.ID {
		st.hostErr = errors.New("unexpected frame instead of a callback reply")
		return nil, st.hostErr
	}
	return f.Reply, nil
}
