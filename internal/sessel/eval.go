package sessel

import (
	"context"
	"math"
	"strings"
	"time"
)

// run is the state shared by every evaluator taking part in one top-level
// evaluation (method bodies, computed properties and defaults evaluated from
// it share the budget, depth and Context).
type run struct {
	ctx    context.Context
	budget *Budget
	depth  int
	host   Host
	cctx   *Dict // the Context object
}

// evaluator evaluates one program in one environment.
type evaluator struct {
	r       *run
	env     *Env
	prog    *program
	classes map[string]Value // resolved @schema names
	decls   map[string]decl
}

// scope is a persistent linked list of bindings; every let/parameter adds
// a node, so closures keep the bindings they captured (R-SESSEL-61/63). A
// node with from != nil marks a from-block's default selector scope.
type scope struct {
	parent *scope
	name   string
	val    Value
	from   *rootSet
}

func (s *scope) bind(name string, v Value) *scope { return &scope{parent: s, name: name, val: v} }

func (s *scope) lookup(name string) (Value, bool) {
	for x := s; x != nil; x = x.parent {
		if x.from == nil && x.name == name {
			return x.val, true
		}
	}
	return nil, false
}

func (s *scope) set(name string, v Value) {
	for x := s; x != nil; x = x.parent {
		if x.from == nil && x.name == name {
			x.val = v
			return
		}
	}
}

func (s *scope) fromRoots() *rootSet {
	for x := s; x != nil; x = x.parent {
		if x.from != nil {
			return x.from
		}
	}
	return nil
}

// returnSignal carries `return` to the enclosing function frame.
type returnSignal struct{ v Value }

func (*returnSignal) Error() string { return "return outside a function" }

func newEvaluator(r *run, env *Env, prog *program) *evaluator {
	ev := &evaluator{r: r, env: env, prog: prog, classes: map[string]Value{}, decls: map[string]decl{}}
	for _, d := range prog.decls {
		ev.decls[d.name] = d
	}
	return ev
}

// step charges one operation (R-SESSEL-356) and checks time and
// cancellation periodically.
func (r *run) step() error {
	b := r.budget
	if b.exhausted != nil {
		return b.exhausted
	}
	b.Ops++
	if b.MaxOps > 0 && b.Ops > b.MaxOps {
		b.exhausted = &Error{Type: RuntimeErrorType, Message: "budget exhausted: operation limit reached", Reason: ReasonBudget}
		return b.exhausted
	}
	if b.Ops&255 == 0 {
		return r.checkTime()
	}
	return nil
}

func (r *run) checkTime() error {
	b := r.budget
	if !b.Deadline.IsZero() && time.Now().After(b.Deadline) {
		b.exhausted = &Error{Type: RuntimeErrorType, Message: "budget exhausted: evaluation time limit reached", Reason: ReasonTimeout}
		return b.exhausted
	}
	if r.ctx != nil {
		if err := r.ctx.Err(); err != nil {
			b.exhausted = &Error{Type: RuntimeErrorType, Message: "evaluation cancelled: " + err.Error(), Reason: ReasonTimeout}
			return b.exhausted
		}
	}
	return nil
}

// alloc charges memory (approximate bytes).
func (r *run) alloc(n int) error {
	b := r.budget
	if b.exhausted != nil {
		return b.exhausted
	}
	b.Mem += int64(n)
	if b.MaxMem > 0 && b.Mem > b.MaxMem {
		b.exhausted = &Error{Type: RuntimeErrorType, Message: "budget exhausted: memory limit reached", Reason: ReasonBudget}
		return b.exhausted
	}
	return nil
}

// ops charges n operations at once (selector matching).
func (r *run) ops(n int) error {
	b := r.budget
	if b.exhausted != nil {
		return b.exhausted
	}
	b.Ops += int64(n)
	if b.MaxOps > 0 && b.Ops > b.MaxOps {
		b.exhausted = &Error{Type: RuntimeErrorType, Message: "budget exhausted: operation limit reached", Reason: ReasonBudget}
		return b.exhausted
	}
	return r.checkTime()
}

func (r *run) enter() error {
	r.depth++
	max := r.budget.MaxDepth
	if max <= 0 {
		max = DefaultMaxDepth
	}
	if r.depth > max {
		r.depth--
		return &Error{Type: RuntimeErrorType, Message: "evaluation depth limit reached", Reason: ReasonDepth}
	}
	return nil
}

func (r *run) leave() { r.depth-- }

// runProgram evaluates the program body.
func (ev *evaluator) runProgram() (Value, error) {
	v, err := ev.evalBlock(ev.prog.body, nil)
	if rs, ok := err.(*returnSignal); ok {
		return rs.v, nil
	}
	return v, err
}

func (ev *evaluator) evalBlock(b *blockNode, sc *scope) (Value, error) {
	var last Value
	for _, st := range b.stmts {
		switch s := st.(type) {
		case *letNode:
			if err := ev.r.step(); err != nil {
				return nil, err
			}
			var err error
			sc, err = ev.evalLet(s, sc)
			if err != nil {
				return nil, err
			}
			last = nil
		case *assignNode:
			if err := ev.evalAssign(s, sc); err != nil {
				return nil, err
			}
			last = nil
		default:
			v, err := ev.eval(st, sc)
			if err != nil {
				return nil, err
			}
			last = v
		}
	}
	return last, nil
}

func (ev *evaluator) evalLet(s *letNode, sc *scope) (*scope, error) {
	if s.pat.name != "" {
		if _, isLambda := s.val.(*lambdaNode); isLambda {
			// A let-bound lambda sees its own binding (recursion).
			ns := sc.bind(s.pat.name, nil)
			v, err := ev.eval(s.val, ns)
			if err != nil {
				return nil, err
			}
			ns.val = v
			return ns, nil
		}
		v, err := ev.eval(s.val, sc)
		if err != nil {
			return nil, err
		}
		return sc.bind(s.pat.name, v), nil
	}
	v, err := ev.eval(s.val, sc)
	if err != nil {
		return nil, err
	}
	return ev.bindPattern(s.pat, v, sc)
}

// bindPattern destructures v (R-SESSEL-69).
func (ev *evaluator) bindPattern(p pattern, v Value, sc *scope) (*scope, error) {
	switch {
	case p.isDict:
		for _, f := range p.fields {
			var fv Value
			if v != nil {
				var err error
				fv, err = ev.member(v, f.key, 0)
				if err != nil {
					return nil, err
				}
			}
			sc = sc.bind(f.name, fv)
		}
		return sc, nil
	case p.isList:
		var items List
		switch x := v.(type) {
		case nil:
		case List:
			items = x
		default:
			return nil, typeErr("cannot destructure %s as a List", TypeName(v))
		}
		for i, name := range p.items {
			var iv Value
			if i < len(items) {
				iv = items[i]
			}
			sc = sc.bind(name, iv)
		}
		if p.rest != "" {
			rest := List{}
			if len(items) > len(p.items) {
				rest = append(List{}, items[len(p.items):]...)
			}
			sc = sc.bind(p.rest, rest)
		}
		return sc, nil
	}
	return sc.bind(p.name, v), nil
}

// evalAssign implements property assignment (R-SESSEL-71).
func (ev *evaluator) evalAssign(s *assignNode, sc *scope) error {
	if err := ev.r.step(); err != nil {
		return err
	}
	var container Value
	var key string
	switch t := s.target.(type) {
	case *memberNode:
		c, err := ev.eval(t.x, sc)
		if err != nil {
			return err
		}
		container, key = c, t.name
	case *indexNode:
		c, err := ev.eval(t.x, sc)
		if err != nil {
			return err
		}
		k, err := ev.eval(t.idx, sc)
		if err != nil {
			return err
		}
		ks, ok := k.(string)
		if !ok {
			if _, isDict := c.(*Dict); isDict {
				return typeErr("dictionary keys are Strings, not %s", TypeName(k))
			}
			return typeErr("cannot assign to an index of %s", TypeName(c))
		}
		container, key = c, ks
	}
	v, err := ev.eval(s.val, sc)
	if err != nil {
		return err
	}
	switch c := container.(type) {
	case *Dict:
		c.Set(key, v)
		return ev.r.alloc(48)
	case *Element:
		if c.Class != nil {
			return ev.setInstanceProperty(c, key, v)
		}
	}
	return typeErr("cannot assign property %q of %s", key, TypeName(container))
}

// eval evaluates an expression node.
func (ev *evaluator) eval(n node, sc *scope) (Value, error) {
	if err := ev.r.step(); err != nil {
		return nil, err
	}
	switch x := n.(type) {
	case *litNode:
		return x.val, nil
	case *strNode:
		return ev.evalString(x, sc)
	case *identNode:
		return ev.lookup(x.name, sc)
	case *listNode:
		out := make(List, 0, len(x.items))
		for _, it := range x.items {
			v, err := ev.eval(it, sc)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, ev.r.alloc(16 * len(out))
	case *dictNode:
		d := NewDict()
		for i, k := range x.keys {
			v, err := ev.eval(x.vals[i], sc)
			if err != nil {
				return nil, err
			}
			d.Set(k, v)
		}
		return d, ev.r.alloc(48 * len(x.keys))
	case *unaryNode:
		v, err := ev.eval(x.x, sc)
		if err != nil {
			return nil, err
		}
		if x.op == tBang {
			return !Truthy(v), nil
		}
		return negate(v)
	case *binaryNode:
		return ev.evalBinary(x, sc)
	case *ternaryNode:
		c, err := ev.eval(x.c, sc)
		if err != nil {
			return nil, err
		}
		if Truthy(c) {
			return ev.eval(x.a, sc)
		}
		return ev.eval(x.b, sc)
	case *isaNode:
		v, err := ev.eval(x.x, sc)
		if err != nil {
			return nil, err
		}
		return ev.evalIsa(v, x.path, sc)
	case *memberNode:
		v, err := ev.eval(x.x, sc)
		if err != nil || v == nil {
			return nil, err
		}
		return ev.member(v, x.name, x.pos)
	case *methodNode:
		return ev.evalMethod(x, sc)
	case *indexNode:
		v, err := ev.eval(x.x, sc)
		if err != nil || v == nil {
			return nil, err
		}
		k, err := ev.eval(x.idx, sc)
		if err != nil {
			return nil, err
		}
		return ev.index(v, k)
	case *subSelectNode:
		v, err := ev.eval(x.x, sc)
		if err != nil || v == nil {
			return nil, err
		}
		return ev.subSelect(v, x.sel, sc)
	case *callNode:
		return ev.evalCall(x, sc)
	case *selectorNode:
		return ev.evalSelector(x, sc)
	case *priorNode:
		if ev.env.Prior == nil {
			return nil, nil
		}
		return ev.env.Prior, nil
	case *fromBlockNode:
		roots, err := ev.evalSources(x.sources, sc)
		if err != nil {
			return nil, err
		}
		return ev.evalBlock(x.body, &scope{parent: sc, from: roots})
	case *blockNode:
		return ev.evalBlock(x, sc)
	case *lambdaNode:
		return &Lambda{params: x.params, rest: x.rest, body: x.body, scope: sc, env: ev}, ev.r.alloc(64)
	case *ifNode:
		for i, c := range x.conds {
			cv, err := ev.eval(c, sc)
			if err != nil {
				return nil, err
			}
			if Truthy(cv) {
				return ev.evalBlock(x.blocks[i], sc)
			}
		}
		if x.els != nil {
			return ev.evalBlock(x.els, sc)
		}
		return nil, nil
	case *tryNode:
		v, err := ev.evalBlock(x.body, sc)
		if err == nil {
			return v, nil
		}
		e, ok := err.(*Error)
		if !ok {
			return nil, err
		}
		return ev.evalBlock(x.catch, sc.bind(x.name, errorValue(e)))
	case *returnNode:
		var v Value
		if x.x != nil {
			var err error
			if v, err = ev.eval(x.x, sc); err != nil {
				return nil, err
			}
		}
		return nil, &returnSignal{v: v}
	case *throwNode:
		v, err := ev.eval(x.x, sc)
		if err != nil {
			return nil, err
		}
		return nil, throwError(v)
	case *newNode:
		return ev.evalNew(x, sc)
	case *letNode, *assignNode:
		// only reachable through a let-expression block
		b := &blockNode{stmts: []node{n}}
		return ev.evalBlock(b, sc)
	}
	return nil, runtimeErr("cannot evaluate %T", n)
}

func (ev *evaluator) evalString(x *strNode, sc *scope) (Value, error) {
	if len(x.parts) == 1 && x.parts[0].expr == nil {
		return x.parts[0].lit, nil
	}
	var b strings.Builder
	for _, p := range x.parts {
		if p.expr == nil {
			b.WriteString(p.lit)
			continue
		}
		v, err := ev.eval(p.expr, sc)
		if err != nil {
			return nil, err
		}
		b.WriteString(TextOf(v))
	}
	if len(x.parts) == 0 {
		return "", nil
	}
	return b.String(), ev.r.alloc(b.Len())
}

// lookup resolves an identifier (R-SESSEL-60).
func (ev *evaluator) lookup(name string, sc *scope) (Value, error) {
	if v, ok := sc.lookup(name); ok {
		return v, nil
	}
	if v, ok, err := ev.declValue(name); ok || err != nil {
		return v, err
	}
	if v, ok, err := ev.hostName(name); ok || err != nil {
		return v, err
	}
	if t, ok := typeNamespaces[name]; ok && !strings.Contains(name, ".") {
		return t, nil
	}
	return nil, &Error{Type: RuntimeErrorType, Message: "unresolved variable: " + name, Reason: ReasonUnresolved}
}

// resolvable reports whether name resolves through steps 1–4 (used by the
// unquoted-selector-value fallback, R-SESSEL-202).
func (ev *evaluator) resolvable(name string, sc *scope) bool {
	if _, ok := sc.lookup(name); ok {
		return true
	}
	if _, ok := ev.decls[name]; ok {
		return true
	}
	switch name {
	case "self", "document":
		return ev.env.HasSelf
	case "request":
		return ev.env.Request != nil
	case "Context", "prior":
		return true
	}
	if _, ok := ev.env.Vars[name]; ok {
		return true
	}
	_, ok := typeNamespaces[name]
	return ok && !strings.Contains(name, ".")
}

func (ev *evaluator) declValue(name string) (Value, bool, error) {
	d, ok := ev.decls[name]
	if !ok {
		return nil, false, nil
	}
	if v, ok := ev.classes[name]; ok {
		return v, true, nil
	}
	var v Value
	if d.kind == "namespace" {
		v = d.url
	} else {
		var err error
		if v, err = ev.classFor(d.url); err != nil {
			return nil, true, err
		}
	}
	ev.classes[name] = v
	return v, true, nil
}

// classFor resolves a type URL to its class value, with the built-in
// classes of R-SESSEL-280.
func (ev *evaluator) classFor(url string) (Value, error) {
	switch url {
	case URLContext:
		return ev.r.cctx, nil
	case URLSelector:
		return typeNamespaces["Selector"], nil
	case URLPlatform:
		return platformClass, nil
	case URLSessel:
		return reflectionClass, nil
	case URLHTTPResponse:
		return httpResponseClass, nil
	case URLPair:
		return pairClass, nil
	}
	return ev.lookupClass(url)
}

// lookupClass asks the host for a schema class, falling back to a bare
// class for undeclared URLs.
func (ev *evaluator) lookupClass(url string) (Class, error) {
	if ev.r.host != nil {
		c, err := ev.r.host.Class(ev.r.ctx, url)
		if err != nil {
			return nil, err
		}
		if c != nil {
			return c, nil
		}
	}
	switch url {
	case URLHTTPResponse:
		return httpResponseClass, nil
	case URLPair:
		return pairClass, nil
	}
	return &BasicClass{TypeURL: url}, nil
}

func (ev *evaluator) hostName(name string) (Value, bool, error) {
	env := ev.env
	switch name {
	case "self":
		if !env.HasSelf {
			return nil, true, &Error{Type: RuntimeErrorType, Message: "self is unbound in this context", Reason: ReasonSelfUnbound}
		}
		return env.Self, true, nil
	case "document":
		if env.Document != nil {
			return env.Document, true, nil
		}
		if !env.HasSelf {
			return nil, true, &Error{Type: RuntimeErrorType, Message: "document is unbound in this context", Reason: ReasonSelfUnbound}
		}
		self := env.Self
		if l, ok := self.(List); ok {
			// validators and resolvers: self is the property's value elements
			for _, it := range l {
				if el, ok := it.(*Element); ok && el.Doc != nil {
					return el.Doc.Element(), true, nil
				}
			}
			return nil, true, &Error{Type: RuntimeErrorType, Message: "document is unbound in this context", Reason: ReasonSelfUnbound}
		}
		if el, ok := self.(*Element); ok {
			if el.isDocRoot() || el.Doc == nil {
				return el, true, nil
			}
			return el.Doc.Element(), true, nil
		}
		return self, true, nil
	case "prior":
		if env.Prior == nil {
			return nil, true, nil
		}
		return env.Prior, true, nil
	case "request":
		if env.Request != nil {
			return env.Request, true, nil
		}
	case "Context":
		return ev.r.cctx, true, nil
	}
	if v, ok := env.Vars[name]; ok {
		return v, true, nil
	}
	return nil, false, nil
}

func negate(v Value) (Value, error) {
	switch x := v.(type) {
	case int64:
		if x == math.MinInt64 {
			return nil, runtimeErr("integer overflow")
		}
		return -x, nil
	case float64:
		return -x, nil
	}
	return nil, typeErr("cannot negate %s", TypeName(v))
}

func (ev *evaluator) evalBinary(x *binaryNode, sc *scope) (Value, error) {
	l, err := ev.eval(x.l, sc)
	if err != nil {
		return nil, err
	}
	switch x.op {
	case tAnd:
		if !Truthy(l) {
			return false, nil
		}
		r, err := ev.eval(x.r, sc)
		if err != nil {
			return nil, err
		}
		return Truthy(r), nil
	case tOr:
		if Truthy(l) {
			return true, nil
		}
		r, err := ev.eval(x.r, sc)
		if err != nil {
			return nil, err
		}
		return Truthy(r), nil
	case tCoal:
		if l != nil {
			return l, nil
		}
		return ev.eval(x.r, sc)
	}
	r, err := ev.eval(x.r, sc)
	if err != nil {
		return nil, err
	}
	switch x.op {
	case tEq:
		return Equal(l, r), nil
	case tNe:
		return !Equal(l, r), nil
	case tLt, tGt, tLe, tGe, tCmp:
		if l == nil || r == nil {
			if x.op == tCmp {
				return nil, nil
			}
			return false, nil
		}
		c, ok := compareValues(l, r)
		if !ok {
			return nil, typeErr("Cannot compare %s and %s", TypeName(l), TypeName(r))
		}
		switch x.op {
		case tLt:
			return c < 0, nil
		case tGt:
			return c > 0, nil
		case tLe:
			return c <= 0, nil
		case tGe:
			return c >= 0, nil
		}
		return int64(c), nil
	case tPlus:
		if ls, ok := l.(string); ok {
			if rs, ok := r.(string); ok {
				return ls + rs, ev.r.alloc(len(ls) + len(rs))
			}
		}
		return arith(tPlus, l, r)
	}
	return arith(x.op, l, r)
}

// arith implements + - * / on numbers (R-SESSEL-80/81).
func arith(op tokKind, l, r Value) (Value, error) {
	li, lInt := l.(int64)
	ri, rInt := r.(int64)
	if lInt && rInt {
		switch op {
		case tPlus:
			s := li + ri
			if (s > li) != (ri > 0) {
				return nil, runtimeErr("integer overflow")
			}
			return s, nil
		case tMinus:
			s := li - ri
			if (s < li) != (ri > 0) {
				return nil, runtimeErr("integer overflow")
			}
			return s, nil
		case tStar:
			if li == 0 || ri == 0 {
				return int64(0), nil
			}
			p := li * ri
			if p/ri != li || (li == -1 && ri == math.MinInt64) || (ri == -1 && li == math.MinInt64) {
				return nil, runtimeErr("integer overflow")
			}
			return p, nil
		case tSlash:
			if ri == 0 {
				return nil, runtimeErr("division by zero")
			}
			if li == math.MinInt64 && ri == -1 {
				return nil, runtimeErr("integer overflow")
			}
			return li / ri, nil
		}
	}
	lf, lok := toFloat(l)
	rf, rok := toFloat(r)
	if !lok || !rok {
		if l != nil && r != nil {
			// Live PageLove (2026-09-29) names the first non-numeric
			// operand; numeric Strings are not coerced either.
			bad := l
			if lok {
				bad = r
			}
			return nil, typeErr("type error: cannot use '%s' in arithmetic", arithText(bad))
		}
		switch op {
		case tPlus:
			return nil, typeErr("Cannot add %s and %s", TypeName(l), TypeName(r))
		case tMinus:
			return nil, typeErr("Cannot subtract %s from %s", TypeName(r), TypeName(l))
		case tStar:
			return nil, typeErr("Cannot multiply %s by %s", TypeName(l), TypeName(r))
		default:
			return nil, typeErr("Cannot divide %s by %s", TypeName(l), TypeName(r))
		}
	}
	var f float64
	switch op {
	case tPlus:
		f = lf + rf
	case tMinus:
		f = lf - rf
	case tStar:
		f = lf * rf
	case tSlash:
		if rf == 0 {
			return nil, runtimeErr("division by zero")
		}
		f = lf / rf
	}
	return checkFloat(f)
}

// arithText quotes a non-numeric operand in the arithmetic TypeError: its
// text representation, with List items joined by ", " (live 2026-09-29:
// [1, "b"] → '1, b', {a: 1} → '{"a":1}').
func arithText(v Value) string {
	if l, ok := v.(List); ok {
		parts := make([]string, len(l))
		for i, it := range l {
			parts[i] = arithText(it)
		}
		return strings.Join(parts, ", ")
	}
	return TextOf(v)
}

func checkFloat(f float64) (Value, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, runtimeErr("numeric overflow")
	}
	return f, nil
}

func toFloat(v Value) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

// evalIsa implements `v isa T` (R-SESSEL-88).
func (ev *evaluator) evalIsa(v Value, path []string, sc *scope) (Value, error) {
	var t Value
	if len(path) >= 1 {
		var err error
		if t, err = ev.lookup(path[0], sc); err != nil {
			return nil, err
		}
		for _, p := range path[1:] {
			if t, err = ev.member(t, p, 0); err != nil {
				return nil, err
			}
		}
	}
	switch tt := t.(type) {
	case *TypeNS:
		return isaNamespace(v, tt.Name), nil
	case Class:
		el, ok := v.(*Element)
		if !ok {
			return false, nil
		}
		return ev.elementIsA(el, tt.URL()), nil
	case *Dict:
		if tt == ev.r.cctx {
			return v == tt, nil
		}
	}
	if t == nil {
		return nil, runtimeErr("isa: %s does not name a type", strings.Join(path, "."))
	}
	return nil, typeErr("isa: %s is not a type", strings.Join(path, "."))
}

func isaNamespace(v Value, name string) bool {
	switch name {
	case "Number":
		_, i := v.(int64)
		_, f := v.(float64)
		return i || f
	case "Integer":
		_, ok := v.(int64)
		return ok
	case "Float":
		_, ok := v.(float64)
		return ok
	case "String":
		_, ok := v.(string)
		return ok
	case "Bool":
		_, ok := v.(bool)
		return ok
	case "Null":
		return v == nil
	case "List":
		_, ok := v.(List)
		return ok
	case "Map":
		_, ok := v.(*Dict)
		return ok
	case "Element":
		_, ok := v.(*Element)
		return ok
	case "Document":
		el, ok := v.(*Element)
		return ok && el.isDocRoot()
	case "Selector":
		_, ok := v.(*SelectorValue)
		return ok
	case "Class":
		_, ok := v.(Class)
		return ok
	case "Instance":
		el, ok := v.(*Element)
		return ok && el.Class != nil
	case "Temporal":
		_, ok := v.(temporal)
		return ok
	}
	if strings.HasPrefix(name, "Temporal.") {
		t, ok := v.(temporal)
		return ok && "Temporal."+t.temporalType() == name
	}
	return false
}

// elementIsA reports whether el's itemtype is url or a schema descendant.
func (ev *evaluator) elementIsA(el *Element, url string) bool {
	types := strings.Fields(attrOr(el.Node, "itemtype", ""))
	if len(types) == 0 && el.Class != nil {
		types = []string{el.Class.URL()}
	}
	for _, t := range types {
		if ev.isA(t, url) {
			return true
		}
	}
	return false
}

// isA reports whether type URL t is target or reaches it through the
// schema parent chain (reflexive, R-MOD-12).
func (ev *evaluator) isA(t, target string) bool {
	seen := map[string]bool{}
	for t != "" && !seen[t] {
		if t == target {
			return true
		}
		seen[t] = true
		if target == URLInstance {
			return true
		}
		c, err := ev.lookupClass(t)
		if err != nil || c == nil {
			return false
		}
		t = c.Parent()
	}
	return false
}

func (ev *evaluator) evalCall(x *callNode, sc *scope) (Value, error) {
	f, err := ev.lookup(x.name, sc)
	if err != nil {
		if e, ok := err.(*Error); ok && e.Reason == ReasonUnresolved && x.name == "size" && len(x.args) == 1 {
			v, err := ev.eval(x.args[0], sc)
			if err != nil || v == nil {
				return nil, err
			}
			return ev.callMethod(v, "count", nil)
		}
		return nil, err
	}
	args, err := ev.evalArgs(x.args, sc)
	if err != nil {
		return nil, err
	}
	switch fn := f.(type) {
	case *Lambda:
		return ev.callLambda(fn, args)
	case *TypeNS:
		switch fn.Name {
		case "String", "Number", "Integer", "Float", "Bool":
			if len(args) != 1 {
				return nil, typeErr("%s() takes one argument", fn.Name)
			}
			if args[0] == nil {
				return nil, nil
			}
			return ev.callMethod(args[0], fn.Name, nil)
		}
	}
	return nil, typeErr("%s is not callable (%s)", x.name, TypeName(f))
}

func (ev *evaluator) evalArgs(ns []node, sc *scope) ([]Value, error) {
	if len(ns) == 0 {
		return nil, nil
	}
	out := make([]Value, len(ns))
	for i, a := range ns {
		v, err := ev.eval(a, sc)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// callLambda invokes a lambda (R-SESSEL-64).
func (ev *evaluator) callLambda(l *Lambda, args []Value) (Value, error) {
	if err := ev.r.enter(); err != nil {
		return nil, err
	}
	defer ev.r.leave()
	if l.native != nil {
		return l.native(&Call{Ctx: ev.r.ctx, ev: ev}, args)
	}
	owner := l.env
	if owner == nil {
		owner = ev
	}
	sc := l.scope
	for i, p := range l.params {
		var v Value
		if i < len(args) {
			v = args[i]
		}
		sc = sc.bind(p, v)
	}
	if l.rest != "" {
		rest := List{}
		if len(args) > len(l.params) {
			rest = append(List{}, args[len(l.params):]...)
		}
		sc = sc.bind(l.rest, rest)
	}
	v, err := owner.eval(l.body, sc)
	if rs, ok := err.(*returnSignal); ok {
		return rs.v, nil
	}
	return v, err
}

func (ev *evaluator) evalMethod(x *methodNode, sc *scope) (Value, error) {
	recv, err := ev.eval(x.x, sc)
	if err != nil || recv == nil {
		return nil, err // null propagation: arguments are not evaluated
	}
	if d, ok := recv.(*Dict); ok && x.name == "get" {
		// .get(key, default) evaluates default lazily (R-SESSEL-187)
		if len(x.args) != 2 {
			return nil, typeErr("get() takes a key and a default")
		}
		k, err := ev.eval(x.args[0], sc)
		if err != nil {
			return nil, err
		}
		ks, ok := k.(string)
		if !ok {
			return nil, typeErr("get(): keys are Strings, not %s", TypeName(k))
		}
		if v, ok := d.Get(ks); ok {
			return v, nil
		}
		return ev.eval(x.args[1], sc)
	}
	args, err := ev.evalArgs(x.args, sc)
	if err != nil {
		return nil, err
	}
	return ev.callMethod(recv, x.name, args)
}

// index implements e[k] (R-SESSEL-74).
func (ev *evaluator) index(v, k Value) (Value, error) {
	switch x := v.(type) {
	case *Dict:
		ks, ok := k.(string)
		if !ok {
			return nil, typeErr("dictionary keys are Strings, not %s", TypeName(k))
		}
		return x.Lookup(ks), nil
	case List:
		i, ok := k.(int64)
		if !ok {
			return nil, typeErr("list index must be an Integer, not %s", TypeName(k))
		}
		return listAt(x, i), nil
	case *Element:
		ks, ok := k.(string)
		if !ok {
			return nil, typeErr("element members are named by Strings, not %s", TypeName(k))
		}
		return ev.member(x, ks, 0)
	}
	return nil, typeErr("cannot index %s", TypeName(v))
}

func listAt(l List, i int64) Value {
	if i < 0 {
		i += int64(len(l))
	}
	if i < 0 || i >= int64(len(l)) {
		return nil
	}
	return l[i]
}

// member implements e.name without parentheses (R-SESSEL-75).
func (ev *evaluator) member(v Value, name string, pos int) (Value, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case *Dict:
		return x.Lookup(name), nil
	case *Element:
		if x.Class != nil {
			return ev.instanceProperty(x, name)
		}
		return ev.elementProperty(x, name), nil
	case temporal:
		return temporalAccessor(x, name)
	case *TypeNS:
		return ev.namespaceMember(x, name)
	case Class:
		return ev.classMember(x, name)
	}
	return nil, typeErr("%s has no member %q", TypeName(v), name)
}

// Call is the handle passed to native functions and Class implementations:
// it evaluates further Sessel within the same run (sharing budget, depth
// and Context).
type Call struct {
	Ctx context.Context
	ev  *evaluator
}

// Host returns the evaluation's host (may be nil).
func (c *Call) Host() Host { return c.ev.r.host }

// Env returns the evaluation environment.
func (c *Call) Env() *Env { return c.ev.env }

// Context returns the shared Context object.
func (c *Call) Context() *Dict { return c.ev.r.cctx }

// CallLambda invokes a lambda value.
func (c *Call) CallLambda(l *Lambda, args ...Value) (Value, error) { return c.ev.callLambda(l, args) }

// IsA reports whether type URL t is target or a schema descendant of it.
func (c *Call) IsA(t, target string) bool { return c.ev.isA(t, target) }

// Class resolves a type URL (a BasicClass when no schema declares it).
func (c *Call) Class(url string) (Class, error) { return c.ev.lookupClass(url) }

// Eval evaluates src (compiled and cached) within this run, with self bound
// to self (when hasSelf) and locals as further context names. It is how
// classes evaluate method bodies, computed properties and defaults.
func (c *Call) Eval(src string, self Value, hasSelf bool, locals map[string]Value) (Value, error) {
	prog, err := compile(src)
	if err != nil {
		return nil, err
	}
	if err := c.ev.r.enter(); err != nil {
		return nil, err
	}
	defer c.ev.r.leave()
	parent := c.ev.env
	env := &Env{Host: parent.Host, Self: self, HasSelf: hasSelf, Context: c.ev.r.cctx, Request: parent.Request,
		Vars: locals, Budget: c.ev.r.budget, Location: parent.Location}
	if el, ok := self.(*Element); ok && hasSelf && el.Doc != nil && !el.isDocRoot() {
		env.Document = el.Doc.Element()
	}
	sub := newEvaluator(c.ev.r, env, prog)
	return sub.runProgram()
}

// Select runs a CSS selector (no expression embedding) over the whole site
// in path order and returns the matches as queried elements.
func (c *Call) Select(css string) (List, error) {
	sel, err := c.ev.compileCSS(css, true)
	if err != nil {
		return nil, err
	}
	roots, err := c.ev.siteRoots()
	if err != nil {
		return nil, err
	}
	return c.ev.matchRoots(sel, roots)
}
