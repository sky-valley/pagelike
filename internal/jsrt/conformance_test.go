package jsrt

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
	"gopkg.in/yaml.v3"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/selector"
)

// The harness cases in harness/cases/javascript mostly need composition and
// schemas, which live in other packages. The cases whose page only
// dispatches JavaScript method elements, j: bindings and p:stamp are
// replayed here through a minimal composer, so every server-JS behaviour
// they pin is checked against this package directly.

type hcaseFile struct {
	Cases []hcase `yaml:"cases"`
}

type hcase struct {
	ID     string  `yaml:"id"`
	Status string  `yaml:"status"`
	Steps  []hstep `yaml:"steps"`
}

type hstep struct {
	Request struct {
		Method  string            `yaml:"method"`
		Path    string            `yaml:"path"`
		Headers map[string]string `yaml:"headers"`
		Body    string            `yaml:"body"`
	} `yaml:"request"`
	Expect struct {
		Status          any               `yaml:"status"`
		BodyContains    []string          `yaml:"body_contains"`
		BodyNotContains []string          `yaml:"body_not_contains"`
		HeaderMatches   map[string]string `yaml:"header_matches"`
		HeadersAbsent   []string          `yaml:"headers_absent"`
	} `yaml:"expect"`
}

const casePrefix = "/_pl/t"

func expand(s string) string {
	s = strings.ReplaceAll(s, "$${", "\x01")
	s = strings.ReplaceAll(s, "${P}", casePrefix)
	return strings.ReplaceAll(s, "\x01", "${")
}

// casesNeedingOtherPackages are replayed by the harness only: they need
// composition features beyond method elements, j: and p:stamp.
var casesNeedingOtherPackages = map[string]string{
	"javascript.jbind.docs-example-liquid":                 "Liquid templates",
	"javascript.jbind.interleave-with-sessel":              "e: Sessel bindings",
	"javascript.jbind.reads-resource-binding":              "r: resource bindings",
	"javascript.methods.composition-budget-503":            "the composition dispatch budget",
	"javascript.methods.context-visible-to-later-siblings": "disputed",
}

// caseDefects are cases that cannot pass on any HTML5 server as written.
var caseDefects = map[string]string{
	// The module source contains the string '<script …></script>', whose
	// "</script>" ends the <script itemprop="source"> element when the page
	// is parsed, truncating the module (a parse failure). The case needs
	// "<\/script>" in its JavaScript string.
	"javascript.dom.crossorigin-nullable-enum": "the module source contains a literal </script>",
}

func TestHarnessCasesThroughJSRT(t *testing.T) {
	files := []string{"dom.yaml", "dom-probes-0929.yaml", "globals.yaml", "methods.yaml", "jbind.yaml"}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join("..", "..", "harness", "cases", "javascript", f))
		if err != nil {
			t.Fatal(err)
		}
		var cf hcaseFile
		if err := yaml.Unmarshal(b, &cf); err != nil {
			t.Fatal(err)
		}
		for _, c := range cf.Cases {
			c := c
			t.Run(c.ID, func(t *testing.T) {
				if c.Status == "disputed" {
					t.Skip("disputed claim (the losing reading)")
				}
				if c.Status == "live-divergence" {
					t.Skip("asserts live PageLove where pagelike deliberately differs")
				}
				if why, ok := casesNeedingOtherPackages[c.ID]; ok {
					t.Skip("needs " + why)
				}
				if why, ok := caseDefects[c.ID]; ok {
					t.Skip("case defect: " + why)
				}
				t.Parallel()
				replayCase(t, c)
			})
		}
	}
}

func replayCase(t *testing.T, c hcase) {
	var page string
	for _, s := range c.Steps {
		if s.Request.Method == "PUT" && s.Request.Body != "" {
			page = expand(s.Request.Body)
			break
		}
	}
	if page == "" {
		t.Skip("no page")
	}
	checked := 0
	for _, s := range c.Steps {
		if s.Request.Method != "GET" {
			continue
		}
		doc, err := dom.Parse([]byte(page))
		if err != nil {
			t.Fatal(err)
		}
		cp := newComposer(t, doc, expand(s.Request.Path))
		cp.run()
		want500 := fmt.Sprint(s.Expect.Status) == "500"
		if want500 {
			if len(cp.failures) == 0 {
				t.Errorf("expected composition to fail (500), it succeeded:\n%s", cp.output(""))
			}
			checked++
			continue
		}
		if len(cp.failures) > 0 {
			for _, f := range cp.failures {
				t.Errorf("unexpected failure %s: %s\n%s", f.Variant, f.Message, f.Stack)
			}
			continue
		}
		body := cp.output(s.Request.Headers["Range"])
		for _, w := range s.Expect.BodyContains {
			if w = expand(w); !strings.Contains(body, w) {
				t.Errorf("body lacks %q:\n%s", w, body)
			}
		}
		for _, w := range s.Expect.BodyNotContains {
			if w = expand(w); strings.Contains(body, w) {
				t.Errorf("body contains %q:\n%s", w, body)
			}
		}
		if s.Expect.HeaderMatches["Cache-Control"] == "private" && !cp.tainted {
			t.Errorf("expected the response to be tainted (Cache-Control: private)")
		}
		for _, h := range s.Expect.HeadersAbsent {
			if h == "Cache-Control" && cp.tainted {
				t.Errorf("expected no Cache-Control (the evaluation was tainted)")
			}
		}
		checked++
	}
	if checked == 0 {
		t.Skip("no GET step")
	}
}

// composer is a minimal stand-in for internal/compose: element- and
// attribute-form JavaScript method dispatch (with doesNotUnderstand), j:
// bindings and p:stamp, with Context scoped to subtrees.
type composer struct {
	t        *testing.T
	doc      *html.Node
	req      *Request
	methods  map[string]*testMethod
	schemaNS string
	jsNS     []string
	failures []*Failure
	tainted  bool
	context  context.Context
}

type testMethod struct {
	name    string
	source  string
	params  []string
	element bool // returns https://pagelove.org/Element
}

func newComposer(t *testing.T, doc *html.Node, path string) *composer {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	cp := &composer{t: t, doc: doc, methods: map[string]*testMethod{}, context: ctx}
	u, _ := url.Parse(path)
	q := NewDict()
	for k, v := range u.Query() {
		q.Set(k, v[0])
	}
	cp.req = &Request{Method: "GET", Path: u.Path, Query: q, Headers: NewHeaders(map[string][]string{"Accept": {"text/html"}})}
	dom.Walk(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return true
		}
		if v, _ := dom.Attr(n, "itemtype"); v == "https://pagelove.org/Schema" {
			for _, m := range dom.Elements(allDescendants(n)) {
				if ip, _ := dom.Attr(m, "itemprop"); ip == "type" && cp.schemaNS == "" {
					cp.schemaNS, _ = dom.Attr(m, "content")
				}
			}
		}
		if v, _ := dom.Attr(n, "itemtype"); v == "https://pagelove.org/Method" {
			m := &testMethod{}
			for _, d := range allDescendants(n) {
				if d.Type != html.ElementNode {
					continue
				}
				ip, _ := dom.Attr(d, "itemprop")
				switch {
				case ip == "name" && d.Parent == n:
					m.name, _ = dom.Attr(d, "content")
				case ip == "returns":
					r, _ := dom.Attr(d, "content")
					m.element = r == "https://pagelove.org/Element"
				case ip == "parameter":
					for _, pm := range allDescendants(d) {
						if pip, _ := dom.Attr(pm, "itemprop"); pip == "name" {
							name, _ := dom.Attr(pm, "content")
							m.params = append(m.params, name)
						}
					}
				case ip == "source":
					m.source = dom.TextContent(d)
				}
			}
			cp.methods[m.name] = m
			return false
		}
		return true
	})
	for _, a := range dom.DocumentElement(doc).Attr {
		if strings.HasPrefix(a.Key, "xmlns:") && a.Val == "https://pagelove.org/Binding/JavaScript" {
			cp.jsNS = append(cp.jsNS, strings.TrimPrefix(a.Key, "xmlns:"))
		}
	}
	return cp
}

func allDescendants(n *html.Node) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		out = append(out, c)
		out = append(out, allDescendants(c)...)
	}
	return out
}

func (cp *composer) methodPrefix() string {
	for _, a := range dom.DocumentElement(cp.doc).Attr {
		if strings.HasPrefix(a.Key, "xmlns:") && a.Val == cp.schemaNS && cp.schemaNS != "" {
			return strings.TrimPrefix(a.Key, "xmlns:")
		}
	}
	return "\x00"
}

func (cp *composer) run() {
	main := dom.FindElement(cp.doc, "main")
	root := main
	if root == nil {
		root = dom.Body(cp.doc)
	}
	cp.walk(root, NewDict())
}

func copyDict(d *Dict) *Dict {
	out := NewDict()
	for _, k := range d.Keys() {
		v, _ := d.Get(k)
		out.Set(k, v)
	}
	return out
}

func (cp *composer) ctx() context.Context { return cp.context }

func (cp *composer) walk(n *html.Node, scope *Dict) {
	prefix := cp.methodPrefix()
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type != html.ElementNode {
			c = next
			continue
		}
		s := copyDict(scope)
		// j: bindings, in declaration order.
		var keep []html.Attribute
		for _, a := range c.Attr {
			p, name, ok := strings.Cut(a.Key, ":")
			if ok && contains2(cp.jsNS, p) {
				res, f := rt(cp.t).EvalExpression(cp.ctx(), ExprRequest{Expression: a.Val, Scope: s, Request: cp.req, Document: &Document{Node: c}})
				if f != nil {
					cp.failures = append(cp.failures, f)
					return
				}
				cp.tainted = cp.tainted || res.Tainted
				s.Set(name, res.Value)
				continue
			}
			keep = append(keep, a)
		}
		c.Attr = keep
		// Attribute-form dispatch.
		replaced := false
		for _, a := range append([]html.Attribute(nil), c.Attr...) {
			p, name, ok := strings.Cut(a.Key, ":")
			if !ok || p != prefix {
				continue
			}
			dom.RemoveAttr(c, a.Key)
			m, args := cp.methods[name], []Value{}
			if m == nil {
				m = cp.methods["doesNotUnderstand"]
				if m == nil {
					continue
				}
				args = []Value{name, []Value{a.Val}}
			} else {
				for i := range m.params {
					if i == 0 {
						args = append(args, a.Val)
					} else {
						args = append(args, nil)
					}
				}
			}
			res, f := cp.dispatch(m, c, args, s)
			if f != nil {
				cp.failures = append(cp.failures, f)
				return
			}
			if m.element && res.Value != nil {
				cp.splice(c, res.Value)
				replaced = true
				break
			}
		}
		if replaced {
			c = next
			continue
		}
		// Element-form dispatch.
		if p, name, ok := strings.Cut(c.Data, ":"); ok && p == prefix {
			m := cp.methods[name]
			var args []Value
			if m == nil {
				m = cp.methods["doesNotUnderstand"]
				if m == nil {
					cp.failures = append(cp.failures, &Failure{Variant: VariantInternal, Message: "no method " + name})
					return
				}
				var params []Value
				for _, a := range c.Attr {
					params = append(params, NewDict("name", a.Key, "value", a.Val))
				}
				if params == nil {
					params = []Value{}
				}
				args = []Value{name, params}
			} else {
				for _, pn := range m.params {
					if v, ok := dom.Attr(c, pn); ok {
						args = append(args, v)
					} else {
						args = append(args, nil)
					}
				}
			}
			res, f := cp.dispatch(m, c, args, s)
			if f != nil {
				cp.failures = append(cp.failures, f)
				return
			}
			cp.splice(c, res.Value)
			c = next
			continue
		}
		// p:stamp
		if strings.HasSuffix(c.Data, ":stamp") && len(c.Attr) > 0 {
			v, _ := s.Get(c.Attr[0].Key)
			c.Parent.InsertBefore(&html.Node{Type: html.TextNode, Data: valueText(v)}, c)
			c.Parent.RemoveChild(c)
			c = next
			continue
		}
		cp.walk(c, s)
		c = next
	}
}

func contains2(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (cp *composer) dispatch(m *testMethod, host *html.Node, args []Value, scope *Dict) (*Result, *Failure) {
	if args == nil {
		args = []Value{}
	}
	ctx := copyDict(scope)
	ctx.Set("request", cp.req)
	res, f := rt(cp.t).CallModule(cp.ctx(), CallRequest{Source: m.source, Slot: SlotDispatch, This: &Element{Node: host}, Args: args, Document: &Document{Node: host}, Context: ctx})
	if f != nil {
		return nil, f
	}
	for _, w := range res.ContextWrites {
		scope.Set(w.Name, w.Value)
	}
	return res, nil
}

func valueText(v Value) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case *Element:
		return x.HTML
	case []Value:
		var parts []string
		for _, e := range x {
			parts = append(parts, valueText(e))
		}
		return strings.Join(parts, ",")
	}
	return fmt.Sprint(v)
}

// splice replaces host with a method result (R-JS-60).
func (cp *composer) splice(host *html.Node, v Value) {
	var nodes []*html.Node
	var add func(v Value)
	add = func(v Value) {
		switch x := v.(type) {
		case nil:
		case *Element:
			ns, err := dom.ParseFragment(x.HTML, host.Parent)
			if err != nil {
				cp.t.Fatal(err)
			}
			nodes = append(nodes, ns...)
		case []Value:
			for _, e := range x {
				add(e)
			}
		default:
			nodes = append(nodes, &html.Node{Type: html.TextNode, Data: valueText(v)})
		}
	}
	add(v)
	dom.Replace(host, nodes)
}

func (cp *composer) output(rangeHdr string) string {
	if sel, ok := strings.CutPrefix(rangeHdr, "selector="); ok {
		s, err := selector.Compile(sel)
		if err != nil {
			cp.t.Fatal(err)
		}
		if n := s.MatchFirst(cp.doc); n != nil {
			return dom.OuterHTML(n)
		}
		return ""
	}
	return string(dom.Render(cp.doc))
}
