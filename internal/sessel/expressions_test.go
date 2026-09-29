package sessel_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"gopkg.in/yaml.v3"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/sessel"
)

// expressionsFile is the shared pure-expression test corpus (spec §22).
var expressionsFile = filepath.Join("..", "..", "harness", "cases", "sessel", "expressions.yaml")

// knownWrong lists expression tests whose expectation contradicts the
// normative spec; each entry says why (they are run, reported, and not
// counted as failures). Keep empty unless a test is demonstrably wrong.
var knownWrong = map[string]string{}

type exprTest struct {
	id      string
	raw     map[string]any
	expr    string
	status  string
	hasJSON bool
}

func loadExpressionTests(t *testing.T) []exprTest {
	t.Helper()
	b, err := os.ReadFile(expressionsFile)
	if os.IsNotExist(err) {
		t.Skipf("%s not present", expressionsFile)
	}
	if err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	if err := yaml.Unmarshal(b, &items); err != nil {
		t.Fatalf("parse %s: %v", expressionsFile, err)
	}
	var out []exprTest
	for _, it := range items {
		_, hasJSON := it["expect_json"]
		out = append(out, exprTest{
			id: str(it["id"]), raw: it, expr: str(it["expr"]), status: str(it["status"]), hasJSON: hasJSON,
		})
	}
	return out
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// buildEnv creates the in-memory site of a test: the context document (as
// self), further site documents, prior, request, and the stub class
// registry read from the site's Schema items.
func buildEnv(t *testing.T, tc exprTest) *sessel.Env {
	h := sessel.NewMemHost()
	ctxPath := str(tc.raw["context_path"])
	if ctxPath == "" {
		ctxPath = "/doc.html"
	}
	env := &sessel.Env{Host: h}
	if ch, ok := tc.raw["context_html"]; ok {
		d, err := h.AddHTML(ctxPath, str(ch))
		if err != nil {
			t.Fatal(err)
		}
		env.Self, env.HasSelf = d.Element(), true
	}
	if site, ok := tc.raw["site"].([]any); ok {
		for _, s := range site {
			m := s.(map[string]any)
			p, body := str(m["path"]), str(m["body"])
			if ct := str(m["content_type"]); ct != "" && !strings.HasPrefix(ct, "text/html") {
				h.AddBlob(p, ct, len(body))
				continue
			}
			if _, err := h.AddHTML(p, body); err != nil {
				t.Fatal(err)
			}
		}
	}
	h.Classes = sessel.MicrodataClasses(h.All())
	if ph, ok := tc.raw["prior_html"]; ok {
		root, err := dom.Parse([]byte(str(ph)))
		if err != nil {
			t.Fatal(err)
		}
		env.Prior = (&sessel.Document{Path: ctxPath, Type: "text/html", Root: root}).Element()
	}
	if r, ok := tc.raw["request"].(map[string]any); ok {
		hdr := http.Header{}
		if hm, ok := r["headers"].(map[string]any); ok {
			for k, v := range hm {
				hdr.Add(k, str(v))
			}
		}
		q := url.Values{}
		if qm, ok := r["query"].(map[string]any); ok {
			for k, v := range qm {
				q.Add(k, str(v))
			}
		}
		params := map[string]string{}
		if pm, ok := r["params"].(map[string]any); ok {
			for k, v := range pm {
				params[k] = str(v)
			}
		}
		env.Request = sessel.NewRequest(str(r["method"]), str(r["path"]), hdr, q, params, []byte(str(r["body"])), nil)
	}
	return env
}

// check runs one test and returns "" on success or a failure description.
func check(t *testing.T, tc exprTest) string {
	env := buildEnv(t, tc)
	v, err := sessel.Eval(context.Background(), tc.expr, env)
	if want, ok := tc.raw["expect_error"]; ok {
		w := str(want)
		if err == nil {
			got, _ := sessel.EncodeJSON(v)
			return fmt.Sprintf("expected error %s, got value %s", w, got)
		}
		switch w {
		case "any":
			return ""
		case "parse":
			if sessel.IsParseError(err) {
				return ""
			}
		default:
			if e, ok := sessel.AsError(err); ok && e.Type == w {
				return ""
			}
		}
		return fmt.Sprintf("expected %s error, got %T: %v", w, err, err)
	}
	if err != nil {
		return fmt.Sprintf("unexpected error: %v", err)
	}
	switch {
	case tc.hasJSON:
		got, err := sessel.EncodeJSON(v)
		if err != nil {
			return fmt.Sprintf("encode: %v", err)
		}
		var gv any
		if err := json.Unmarshal(got, &gv); err != nil {
			return fmt.Sprintf("result is not JSON: %s", got)
		}
		want := normalizeYAML(tc.raw["expect_json"])
		if !jsonEqual(gv, want) {
			wb, _ := json.Marshal(want)
			return fmt.Sprintf("got %s, want %s", got, wb)
		}
	case tc.raw["expect_html"] != nil:
		el, ok := v.(*sessel.Element)
		if !ok {
			got, _ := sessel.EncodeJSON(v)
			return fmt.Sprintf("expected an element, got %s", got)
		}
		got := dom.OuterHTML(el.Node)
		if canonHTML(got) != canonHTML(str(tc.raw["expect_html"])) {
			return fmt.Sprintf("got %s, want %s", got, str(tc.raw["expect_html"]))
		}
	case tc.raw["expect_match"] != nil:
		s, ok := v.(string)
		if !ok {
			got, _ := sessel.EncodeJSON(v)
			return fmt.Sprintf("expected a String, got %s", got)
		}
		re, err := regexp.Compile(str(tc.raw["expect_match"]))
		if err != nil {
			return fmt.Sprintf("bad expect_match: %v", err)
		}
		if !re.MatchString(s) {
			return fmt.Sprintf("%q does not match %s", s, re)
		}
	default:
		return "test has no expectation"
	}
	return ""
}

func TestExpressions(t *testing.T) {
	tests := loadExpressionTests(t)
	pass, fail, disputed := 0, 0, 0
	var failed []string
	for _, tc := range tests {
		tc := tc
		msg := check(t, tc)
		switch {
		case tc.status == "disputed":
			disputed++
			if msg == "" {
				t.Logf("disputed test %s unexpectedly holds", tc.id)
			}
			continue
		case knownWrong[tc.id] != "":
			t.Logf("known-wrong test %s: %s (result: %s)", tc.id, knownWrong[tc.id], msg)
			continue
		}
		t.Run(tc.id, func(t *testing.T) {
			if msg != "" {
				t.Errorf("%s\n    expr: %s", msg, tc.expr)
			}
		})
		if msg == "" {
			pass++
		} else {
			fail++
			failed = append(failed, tc.id)
		}
	}
	t.Logf("expression tests: %d passed, %d failed, %d disputed (losing side, not run as failures), %d known-wrong", pass, fail, disputed, len(knownWrong))
}

func normalizeYAML(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = normalizeYAML(e)
		}
		return out
	case map[any]any:
		out := map[string]any{}
		for k, e := range x {
			out[fmt.Sprint(k)] = normalizeYAML(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalizeYAML(e)
		}
		return out
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case uint64:
		return float64(x)
	}
	return v
}

func jsonEqual(a, b any) bool {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !jsonEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if !jsonEqual(v, y[k]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

// canonHTML renders markup DOM-equivalently: whitespace-only text between
// tags dropped, attributes sorted, empty attribute values normalized.
func canonHTML(s string) string {
	nodes, err := dom.ParseBodyFragment(s)
	if err != nil {
		return "!" + err.Error()
	}
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			if strings.TrimSpace(n.Data) != "" {
				b.WriteString(n.Data)
			}
		case html.ElementNode:
			attrs := make([]string, 0, len(n.Attr))
			for _, a := range n.Attr {
				k := a.Key
				if a.Namespace != "" {
					k = a.Namespace + ":" + k
				}
				attrs = append(attrs, k+"="+a.Val)
			}
			sort.Strings(attrs)
			fmt.Fprintf(&b, "<%s|%s %s>", n.Namespace, n.Data, strings.Join(attrs, " "))
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			fmt.Fprintf(&b, "</%s>", n.Data)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return b.String()
}
