package liquid

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/osteele/liquid/expressions"
	"github.com/osteele/liquid/render"
	"github.com/osteele/liquid/values"
)

// The tag set (R-LIQ-70). pagelike defines every tag itself instead of
// calling tags.AddStandardTags: the library cannot redefine a block once
// defined, and `for`, `tablerow`, `case` and `cycle` need Shopify semantics
// it does not have (loop modifier order, nil collections, Shopify tablerow
// markup, numeric-string `when`; gaps G6 and G7). Everything else is small
// enough to own, and owning it lets every tag charge the budget.
//
// Compiled templates are shared by all renders, so tags keep no state of
// their own: per-render state is reached through a binding (stateOf).

// stateKey binds the render state. Template identifiers cannot contain NUL,
// so no template can read or shadow it.
const stateKey = "\x00pagelike"

func stateOf(ctx render.Context) *renderState {
	st, _ := ctx.Get(stateKey).(*renderState)
	return st
}

var (
	errBreak    = errors.New("break outside a loop")
	errContinue = errors.New("continue outside a loop")
)

func addTags(cfg *render.Config) {
	cfg.AddTag("pl_out", outputTag(true))
	cfg.AddTag("pl_out_attr", outputTag(false))
	cfg.AddTag("echo", outputTag(true))
	cfg.AddTag("assign", assignTag)
	cfg.AddTag("cycle", cycleTag)
	cfg.AddTag("increment", counterTag(1))
	cfg.AddTag("decrement", counterTag(-1))
	cfg.AddTag("break", func(string) (func(io.Writer, render.Context) error, error) {
		return func(io.Writer, render.Context) error { return errBreak }, nil
	})
	cfg.AddTag("continue", func(string) (func(io.Writer, render.Context) error, error) {
		return func(io.Writer, render.Context) error { return errContinue }, nil
	})
	// Templates perform no I/O (R-LIQ-26, R-LIQ-77): these fail to compile,
	// and nothing reads a file, document or network resource.
	for _, name := range []string{"include", "include_relative", "render", "layout", "section"} {
		cfg.AddTag(name, func(string) (func(io.Writer, render.Context) error, error) {
			return nil, fmt.Errorf("the %s tag is not supported: templates perform no I/O", name)
		})
	}

	// The parser handles raw and comment itself once they are defined.
	cfg.AddBlock("raw")
	cfg.AddBlock("comment")
	cfg.AddBlock("capture").Compiler(captureCompiler)
	cfg.AddBlock("if").Clause("elsif").Clause("else").Compiler(ifCompiler(true))
	cfg.AddBlock("unless").Clause("elsif").Clause("else").Compiler(ifCompiler(false))
	cfg.AddBlock("case").Clause("when").Clause("else").Compiler(caseCompiler)
	cfg.AddBlock("for").Clause("else").Compiler(loopCompiler(false))
	cfg.AddBlock("tablerow").Compiler(loopCompiler(true))
}

// safeEval evaluates an expression, turning the panics that escape the
// library's evaluator into errors carrying the first line of the message
// only (no stack traces, R-LIQ-209).
func safeEval(ctx render.Context, e expressions.Expression) (v any, err error) {
	defer func() {
		if p := recover(); p != nil {
			v, err = nil, panicError(p)
		}
	}()
	return ctx.Evaluate(e)
}

func panicError(p any) error {
	if err, ok := p.(error); ok && isFatal(err) {
		return err
	}
	return errors.New(firstLine(fmt.Sprint(p)))
}

// outputTag is `{{ expr }}` after preprocessing: a failed expression
// degrades to an inline Error item in markup context and to nothing
// anywhere else; budget exhaustion still fails the render (R-LIQ-200,
// R-LIQ-201, R-LIQ-212).
func outputTag(markup bool) render.TagCompiler {
	return func(args string) (func(io.Writer, render.Context) error, error) {
		expr, err := expressions.Parse(decodeArgs(args))
		if err != nil {
			return nil, err
		}
		return func(w io.Writer, ctx render.Context) error {
			st := stateOf(ctx)
			if err := st.charge(1); err != nil {
				return err
			}
			v, err := safeEval(ctx, expr)
			if err != nil {
				if fatal := st.fatal(err); fatal != nil {
					return fatal
				}
				if markup {
					_, werr := io.WriteString(w, errorMarker(errMessage(err), st.xml))
					return werr
				}
				return nil
			}
			s := toString(v)
			if _, safe := v.(SafeString); !safe && st.opts.AutoEscape && !st.rawHost {
				s = htmlEscaper.Replace(s)
			}
			_, err = io.WriteString(w, s)
			return err
		}, nil
	}
}

// assignTag stores a value, charging the memory it holds. An error in the
// value fails the render (R-LIQ-204).
func assignTag(args string) (func(io.Writer, render.Context) error, error) {
	stmt, err := expressions.ParseStatement(expressions.AssignStatementSelector, args)
	if err != nil {
		return nil, err
	}
	if len(stmt.Assignment.Path) > 1 {
		return nil, errors.New("assign: dot notation (a.b = …) is not supported")
	}
	name, value := stmt.Assignment.Variable, stmt.Assignment.ValueFn
	return func(_ io.Writer, ctx render.Context) error {
		st := stateOf(ctx)
		if err := st.charge(1); err != nil {
			return err
		}
		v, err := safeEval(ctx, value)
		if err != nil {
			return err
		}
		ctx.Set(name, v)
		return st.store(name, memSize(v))
	}, nil
}

// captureCompiler renders the block into a variable. Output inside degrades
// as usual, markers included (R-LIQ-204); the captured bytes are charged
// as they are produced.
func captureCompiler(node render.BlockNode) (func(io.Writer, render.Context) error, error) {
	name := strings.TrimSpace(node.Args)
	if !isIdentifier(name) {
		return nil, fmt.Errorf("capture requires one variable name, got %q", node.Args)
	}
	return func(_ io.Writer, ctx render.Context) error {
		st := stateOf(ctx)
		if err := st.charge(1); err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := ctx.RenderChildren(&memWriter{w: &buf, st: st}); err != nil {
			return err
		}
		ctx.Set(name, buf.String())
		st.stored[name] = max(st.stored[name], int64(buf.Len()))
		return nil
	}, nil
}

func isIdentifier(s string) bool {
	if s == "" || !isIdentStart(s[0]) {
		return false
	}
	return scanIdent(s, 0) == len(s)
}

// ifCompiler compiles if (polarity true) and unless. An error in any
// condition fails the render (R-LIQ-202).
func ifCompiler(polarity bool) render.BlockCompiler {
	return func(node render.BlockNode) (func(io.Writer, render.Context) error, error) {
		type branch struct {
			test   expressions.Expression // nil for else
			negate bool
			body   *render.BlockNode
		}
		first, err := expressions.Parse(node.Args)
		if err != nil {
			return nil, err
		}
		branches := []branch{{first, !polarity, &node}}
		for i, c := range node.Clauses {
			switch c.Name {
			case "elsif":
				t, err := expressions.Parse(c.Args)
				if err != nil {
					return nil, err
				}
				branches = append(branches, branch{t, false, c})
			case "else":
				if i != len(node.Clauses)-1 {
					return nil, fmt.Errorf("%s: else must be the last clause", node.Name)
				}
				branches = append(branches, branch{nil, false, c})
			}
		}
		return func(w io.Writer, ctx render.Context) error {
			if err := stateOf(ctx).charge(1); err != nil {
				return err
			}
			for _, b := range branches {
				if b.test == nil {
					return ctx.RenderBlock(w, b.body)
				}
				v, err := safeEval(ctx, b.test)
				if err != nil {
					return err
				}
				if truthy(v) != b.negate {
					return ctx.RenderBlock(w, b.body)
				}
			}
			return nil
		}, nil
	}
}

// caseCompiler follows Shopify: every `when` that matches renders, and an
// `else` renders when no earlier `when` matched. Matching is `==`, numeric
// strings and blank/empty included (R-LIQ-76).
func caseCompiler(node render.BlockNode) (func(io.Writer, render.Context) error, error) {
	type clause struct {
		values []expressions.Expression // nil for else
		body   *render.BlockNode
	}
	subject, err := expressions.Parse(node.Args)
	if err != nil {
		return nil, err
	}
	var clauses []clause
	for _, c := range node.Clauses {
		switch c.Name {
		case "when":
			stmt, err := expressions.ParseStatement(expressions.WhenStatementSelector, c.Args)
			if err != nil {
				return nil, err
			}
			clauses = append(clauses, clause{stmt.When.Exprs, c})
		case "else":
			clauses = append(clauses, clause{nil, c})
		}
	}
	return func(w io.Writer, ctx render.Context) error {
		if err := stateOf(ctx).charge(1); err != nil {
			return err
		}
		v, err := safeEval(ctx, subject)
		if err != nil {
			return err
		}
		matched := false
		for _, c := range clauses {
			if c.values == nil {
				if !matched {
					if err := ctx.RenderBlock(w, c.body); err != nil {
						return err
					}
				}
				continue
			}
			for _, e := range c.values {
				wv, err := safeEval(ctx, e)
				if err != nil {
					return err
				}
				if looseEqual(v, wv) {
					matched = true
					if err := ctx.RenderBlock(w, c.body); err != nil {
						return err
					}
					break
				}
			}
		}
		return nil
	}, nil
}

// cycleTag keeps one position per group (or per value list) for the whole
// render, like Shopify; it works outside loops too.
func cycleTag(args string) (func(io.Writer, render.Context) error, error) {
	stmt, err := expressions.ParseStatement(expressions.CycleStatementSelector, args)
	if err != nil {
		return nil, err
	}
	group, vals := stmt.Cycle.Group, stmt.Cycle.Values
	key := "group:" + group
	if group == "" {
		key = "values:" + strings.Join(vals, "\x00")
	}
	return func(w io.Writer, ctx render.Context) error {
		st := stateOf(ctx)
		if err := st.charge(1); err != nil {
			return err
		}
		n := st.cycles[key]
		st.cycles[key] = n + 1
		_, err := io.WriteString(w, vals[n%len(vals)])
		return err
	}, nil
}

// counterTag is increment (output, then add one) and decrement (subtract
// one, then output). Counters are shared by the two tags, start at 0, and
// are visible as variables unless assign or capture set the same name.
func counterTag(delta int) render.TagCompiler {
	return func(args string) (func(io.Writer, render.Context) error, error) {
		name := strings.TrimSpace(args)
		if !isIdentifier(name) {
			return nil, fmt.Errorf("counter requires one variable name, got %q", args)
		}
		return func(w io.Writer, ctx render.Context) error {
			st := stateOf(ctx)
			if err := st.charge(1); err != nil {
				return err
			}
			n := st.counters[name]
			out := n
			if delta < 0 {
				n--
				out = n
			} else {
				n++
			}
			st.counters[name] = n
			if _, assigned := st.stored[name]; !assigned {
				ctx.Set(name, n)
			}
			_, err := io.WriteString(w, strconv.Itoa(out))
			return err
		}, nil
	}
}

// sequence is what a loop iterates, without materializing ranges.
type sequence interface {
	Len() int
	At(i int) any
}

type sliceSeq []any

func (s sliceSeq) Len() int     { return len(s) }
func (s sliceSeq) At(i int) any { return s[i] }

type rangeSeq values.Range

func (r rangeSeq) Len() int     { return max(values.Range(r).Len(), 0) }
func (r rangeSeq) At(i int) any { return values.Range(r).Index(i) }

type reflectSeq struct{ v reflect.Value }

func (r reflectSeq) Len() int     { return r.v.Len() }
func (r reflectSeq) At(i int) any { return r.v.Index(i).Interface() }

// window is offset/limit/reversed over a sequence.
type window struct {
	s        sequence
	from, n  int
	reversed bool
}

func (w window) Len() int { return w.n }
func (w window) At(i int) any {
	if w.reversed {
		return w.s.At(w.from + w.n - 1 - i)
	}
	return w.s.At(w.from + i)
}

// loopSequence is what `for` iterates for a value (R-LIQ-71): nil and
// non-collections iterate nothing (iterable false, so `else` runs), a
// non-blank string and an item iterate once, a hash iterates [key, value]
// pairs, a range iterates lazily.
func loopSequence(v any) (sequence, bool) {
	switch x := v.(type) {
	case nil:
		return nil, false
	case []any:
		return sliceSeq(x), true
	case values.Range:
		return rangeSeq(x), true
	case string:
		if strings.TrimSpace(x) == "" {
			return nil, false
		}
		return sliceSeq{x}, true
	case SafeString:
		return loopSequence(string(x))
	case *Item:
		return sliceSeq{x}, true
	case *Hash:
		return pairs(x), true
	case *Request:
		return pairs(x.whole()), true
	case map[string]any:
		return pairs(HashOf(x)), true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		return reflectSeq{rv}, true
	case reflect.Map:
		keys := rv.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j]) })
		out := make(sliceSeq, len(keys))
		for i, k := range keys {
			out[i] = []any{k.Interface(), rv.MapIndex(k).Interface()}
		}
		return out, true
	}
	return nil, false
}

func pairs(h *Hash) sequence {
	out := make(sliceSeq, 0, h.Len())
	for _, k := range h.keys {
		out = append(out, []any{k, h.m[k]})
	}
	return out
}

// loopInt evaluates a loop modifier (limit, offset, cols) as an integer.
func loopInt(ctx render.Context, e expressions.Expression, name string) (int, bool, error) {
	if e == nil {
		return 0, false, nil
	}
	v, err := safeEval(ctx, e)
	if err != nil {
		return 0, false, err
	}
	if v == nil {
		return 0, false, nil
	}
	i, f, isInt, ok := number(v)
	if !ok {
		return 0, false, fmt.Errorf("loop %s must be an integer, got %s", name, clipString(toString(v), 40))
	}
	if !isInt {
		i = int64(f)
	}
	return int(max(min(i, 1<<31), -(1 << 31))), true, nil
}

// loopCompiler compiles for (Shopify semantics, R-LIQ-70 … R-LIQ-72:
// offset and limit apply before reversed; a nil collection runs else) and
// tablerow (Shopify markup). Each iteration is charged to the budget.
func loopCompiler(table bool) render.BlockCompiler {
	return func(node render.BlockNode) (func(io.Writer, render.Context) error, error) {
		stmt, err := expressions.ParseStatement(expressions.LoopStatementSelector, node.Args)
		if err != nil {
			return nil, err
		}
		loop := stmt.Loop
		var elseBody *render.BlockNode
		for _, c := range node.Clauses {
			if elseBody != nil {
				return nil, fmt.Errorf("%s accepts at most one else clause", node.Name)
			}
			elseBody = c
		}
		if !table && loop.Cols != nil {
			return nil, errors.New("for: cols is a tablerow modifier")
		}
		return func(w io.Writer, ctx render.Context) error {
			st := stateOf(ctx)
			if err := st.charge(1); err != nil {
				return err
			}
			coll, err := safeEval(ctx, loop.Expr)
			if err != nil {
				return err
			}
			seq, iterable := loopSequence(coll)
			if !iterable {
				if table && coll != nil {
					seq = sliceSeq{}
				} else {
					if elseBody != nil {
						return ctx.RenderBlock(w, elseBody)
					}
					return nil
				}
			}
			offset, _, err := loopInt(ctx, loop.Offset, "offset")
			if err != nil {
				return err
			}
			limit, hasLimit, err := loopInt(ctx, loop.Limit, "limit")
			if err != nil {
				return err
			}
			from := min(max(offset, 0), seq.Len())
			n := seq.Len() - from
			if hasLimit {
				n = min(n, max(limit, 0))
			}
			win := window{seq, from, n, loop.Reversed}
			if table {
				return renderTableRow(w, ctx, st, loop, win)
			}
			if n == 0 {
				if elseBody != nil {
					return ctx.RenderBlock(w, elseBody)
				}
				return nil
			}
			return renderFor(w, ctx, st, loop.Variable, win)
		}, nil
	}
}

func renderFor(w io.Writer, ctx render.Context, st *renderState, name string, win window) error {
	prevLoop, prevVar := ctx.Get("forloop"), ctx.Get(name)
	defer func() {
		ctx.Set("forloop", prevLoop)
		ctx.Set(name, prevVar)
	}()
	n := win.Len()
	forloop := map[string]any{"length": n, "parentloop": prevLoop}
	ctx.Set("forloop", forloop)
	for i := range n {
		if err := st.charge(1); err != nil {
			return err
		}
		ctx.Set(name, win.At(i))
		forloop["first"] = i == 0
		forloop["last"] = i == n-1
		forloop["index"] = i + 1
		forloop["index0"] = i
		forloop["rindex"] = n - i
		forloop["rindex0"] = n - i - 1
		if err := ctx.RenderChildren(w); err != nil {
			switch {
			case errors.Is(err, errBreak):
				return nil
			case errors.Is(err, errContinue):
				continue
			}
			return err
		}
	}
	return nil
}

// renderTableRow writes Shopify's tablerow markup: rows of `cols` cells
// (all cells in one row by default), with newlines after the opening and
// closing row tags.
func renderTableRow(w io.Writer, ctx render.Context, st *renderState, loop expressions.Loop, win window) error {
	n := win.Len()
	cols, hasCols, err := loopInt(ctx, loop.Cols, "cols")
	if err != nil {
		return err
	}
	if !hasCols || cols <= 0 {
		cols = n
	}
	name := loop.Variable
	prevLoop, prevVar := ctx.Get("tablerowloop"), ctx.Get(name)
	defer func() {
		ctx.Set("tablerowloop", prevLoop)
		ctx.Set(name, prevVar)
	}()
	if _, err := io.WriteString(w, "<tr class=\"row1\">\n"); err != nil {
		return err
	}
	trl := map[string]any{"length": n}
	ctx.Set("tablerowloop", trl)
	col, row := 1, 1
	for i := range n {
		if err := st.charge(1); err != nil {
			return err
		}
		ctx.Set(name, win.At(i))
		trl["index"], trl["index0"] = i+1, i
		trl["rindex"], trl["rindex0"] = n-i, n-i-1
		trl["first"], trl["last"] = i == 0, i == n-1
		trl["col"], trl["col0"], trl["row"] = col, col-1, row
		trl["col_first"], trl["col_last"] = col == 1, col == cols
		if _, err := io.WriteString(w, `<td class="col`+strconv.Itoa(col)+`">`); err != nil {
			return err
		}
		rerr := ctx.RenderChildren(w)
		if _, err := io.WriteString(w, "</td>"); err != nil {
			return err
		}
		if rerr != nil {
			if errors.Is(rerr, errBreak) {
				break
			}
			if !errors.Is(rerr, errContinue) {
				return rerr
			}
		}
		if col == cols && i != n-1 {
			if _, err := io.WriteString(w, "</tr>\n<tr class=\"row"+strconv.Itoa(row+1)+"\">"); err != nil {
				return err
			}
		}
		if col == cols {
			col, row = 1, row+1
		} else {
			col++
		}
	}
	_, err = io.WriteString(w, "</tr>\n")
	return err
}

// memWriter charges bytes written to the memory axis (capture buffers).
type memWriter struct {
	w  io.Writer
	st *renderState
}

func (m *memWriter) Write(p []byte) (int, error) {
	if err := m.st.chargeMemory(int64(len(p))); err != nil {
		return 0, err
	}
	return m.w.Write(p)
}

// memSize approximates the memory a stored value holds (R-LIQ-211): a string
// by its length, a list by the sum of its elements. Site content (items,
// the request) is shared rather than copied and counts as a reference.
func memSize(v any) int64 {
	switch x := v.(type) {
	case nil:
		return 0
	case string:
		return int64(len(x))
	case SafeString:
		return int64(len(x))
	case []any:
		n := int64(8 * len(x))
		for _, e := range x {
			n += memSize(e)
		}
		return n
	case *Hash:
		n := int64(0)
		for _, k := range x.keys {
			n += int64(len(k)) + memSize(x.m[k])
		}
		return n
	case map[string]any:
		n := int64(0)
		for k, e := range x {
			n += int64(len(k)) + memSize(e)
		}
		return n
	}
	return 8
}
