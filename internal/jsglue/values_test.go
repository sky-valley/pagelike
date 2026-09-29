package jsglue

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/sky-valley/pagelike/internal/budget"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/jsrt"
	"github.com/sky-valley/pagelike/internal/sessel"
)

func TestParseElement(t *testing.T) {
	for _, c := range []struct{ in, tag string }{
		{`<td class="x">1</td>`, "td"},
		{`<tr><td>1</td></tr>`, "tr"},
		{`<li>a</li>`, "li"},
		{`<html><head></head><body><p>x</p></body></html>`, "html"},
		{`<body class="b"><p>x</p></body>`, "body"},
		{`  <option value="1">one</option>`, "option"},
	} {
		n := parseElement(c.in)
		if n == nil || n.Data != c.tag || n.Parent != nil {
			t.Errorf("%s: got %v", c.in, n)
		}
	}
	if parseElement("text") != nil {
		t.Error("text is not an element")
	}
}

func TestInstanceMarkup(t *testing.T) {
	inner := &jsrt.Instance{Type: "urn:t:Pos", Props: jsrt.NewDict("x", int64(1), "y", 2.5)}
	inst := &jsrt.Instance{Type: "urn:t:Note", Props: jsrt.NewDict("title", `a "b" <c>`, "tags", []jsrt.Value{"p", "q"}, "at", inner, "none", nil,
		"el", &jsrt.Element{HTML: `<span>s</span>`})}
	got := instanceMarkup(inst, "")
	want := `<div itemscope itemtype="urn:t:Note"><meta itemprop="title" content="a &#34;b&#34; &lt;c&gt;"><meta itemprop="tags" content="p"><meta itemprop="tags" content="q">` +
		`<div itemprop="at" itemscope itemtype="urn:t:Pos"><meta itemprop="x" content="1"><meta itemprop="y" content="2.5"></div><span itemprop="el">s</span></div>`
	if got != want {
		t.Errorf("markup\n got %s\nwant %s", got, want)
	}
	el, ok := toSessel(inst, nil).(*sessel.Element)
	if !ok || dom.AttrOr(el.Node, "itemtype", "") != "urn:t:Note" {
		t.Errorf("instance as Sessel: %#v", toSessel(inst, nil))
	}
}

func TestValueRoundTrips(t *testing.T) {
	// The schema package's JSON-like model in and out.
	in := map[string]any{"b": []any{int64(1), "x", map[string]any{"$type": "element", "$html": "<p>p</p>", "$source": "/a.html"}}, "a": true}
	d, ok := fromGo(in).(*jsrt.Dict)
	if !ok || !reflect.DeepEqual(d.Keys(), []string{"a", "b"}) {
		t.Fatalf("fromGo: %#v", fromGo(in))
	}
	b, _ := d.Get("b")
	if el, ok := b.([]jsrt.Value)[2].(*jsrt.Element); !ok || el.HTML != "<p>p</p>" || el.Source != "/a.html" {
		t.Errorf("tagged element: %#v", b)
	}
	back := toGo(jsrt.NewDict("a", true, "b", []jsrt.Value{int64(1), &jsrt.Element{HTML: "<p>p</p>", Source: "/a.html"}}))
	want := map[string]any{"a": true, "b": []any{int64(1), map[string]any{"$type": "element", "$html": "<p>p</p>", "$source": "/a.html"}}}
	if !reflect.DeepEqual(back, want) {
		t.Errorf("toGo: %#v", back)
	}
	// Sessel values out and back.
	sd := sessel.NewDict()
	sd.Set("z", int64(2))
	sd.Set("a", sessel.List{"x", nil, 1.5})
	jv, err := fromSessel(sd, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rd, ok := toSessel(jv, nil).(*sessel.Dict); !ok || !reflect.DeepEqual(rd.Keys(), []string{"z", "a"}) || !sessel.Equal(rd, sd) {
		t.Errorf("Sessel round trip: %#v", toSessel(jv, nil))
	}
	if v, err := fromSessel(sessel.Instant{NS: 0}, nil); err != nil || v.(jsrt.Temporal).Kind != "Instant" {
		t.Errorf("temporal: %#v %v", v, err)
	}
	if _, err := fromSessel(&sessel.Lambda{}, nil); err == nil {
		t.Error("a lambda has no JavaScript form")
	}
	if _, ok := fromSesselLenient(&sessel.Lambda{}, nil).(jsrt.Undefined); !ok {
		t.Error("lenient conversion reads a lambda as undefined")
	}
}

func TestRequestObject(t *testing.T) {
	anon := sessel.NewRequest("get", "/p", map[string][]string{"X-A": {"1"}}, map[string][]string{"k": {"v", "w"}}, map[string]string{"id": "7"}, nil, nil)
	r := requestOf(anon)
	if r.Method != "GET" || r.Path != "/p" || r.Auth != nil || r.Body != nil {
		t.Errorf("anonymous request: %+v", r)
	}
	if v, _ := r.Query.Get("k"); v != "v" {
		t.Errorf("query: %v", v)
	}
	if v, _ := r.Params.Get("id"); v != "7" {
		t.Errorf("params: %v", v)
	}
	auth := sessel.NewAuth("ada", map[string]any{"email": "a@x"}, []string{"ed"})
	signed := sessel.NewRequest("GET", "/p", nil, nil, nil, nil, auth)
	if a, ok := requestOf(signed).Auth.(*jsrt.Dict); !ok || a.Len() != 4 {
		t.Errorf("signed-in auth: %#v", requestOf(signed).Auth)
	}
}

// An exhausted request budget fails before a worker is taken.
func TestExhaustedBudget(t *testing.T) {
	b := budget.New()
	b.Deadline = time.Now().Add(-time.Second)
	_, f := callModule(budget.With(context.Background(), b), jsrt.CallRequest{Source: "export default () => 1", Slot: jsrt.SlotRead, Args: []jsrt.Value{nil}})
	if f == nil || f.Variant != jsrt.VariantTimeout || !f.Budget() {
		t.Errorf("failure %v", f)
	}
	b = budget.New()
	b.AddJS(0, budget.DefaultMemory)
	_, f = callModule(budget.With(context.Background(), b), jsrt.CallRequest{Source: "export default () => 1", Slot: jsrt.SlotRead, Args: []jsrt.Value{nil}})
	if f == nil || f.Variant != jsrt.VariantOutOfMemory {
		t.Errorf("failure %v", f)
	}
	// A call within budget is charged to the request.
	b = budget.New()
	res, f := callModule(budget.With(context.Background(), b), jsrt.CallRequest{Source: "export default (v) => v + 1", Slot: jsrt.SlotRead, Args: []jsrt.Value{int64(41)}})
	if f != nil || res.Value != int64(42) {
		t.Fatalf("result %v, failure %v", res, f)
	}
	if ops, _, _ := b.Consumed(); ops < 1 {
		t.Errorf("evaluation not charged: ops %d", ops)
	}
}
