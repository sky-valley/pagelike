package jsrt

import (
	"strings"
	"testing"
)

func TestMarshalJSONSesselShape(t *testing.T) {
	v := []Value{
		NewDict("b", int64(1), "a", 2.5, "$type", "x"),
		&Element{HTML: `<p class="a">x & y</p>`, Source: "/d.html"},
		&Element{HTML: "<i></i>"},
		&Instance{Type: "https://x/T", Props: NewDict("n", "v")},
		nil, true, "s<>",
	}
	b, err := MarshalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"b":1,"a":2.5,"$type":"x"},{"$type":"element","$html":"<p class=\"a\">x & y</p>","$source":"/d.html"},{"$type":"element","$html":"<i></i>"},{"$type":"instance","$itemtype":"https://x/T","$props":{"n":"v"}},null,true,"s<>"]`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
	if _, err := MarshalJSON([]Value{nan()}); err == nil {
		t.Error("NaN must not encode")
	}
}

func TestDictHelpers(t *testing.T) {
	d := NewDict("a", 1, "b", 2)
	d.Set("a", 3)
	d.Set("c", 4)
	d.Delete("b")
	if strings.Join(d.Keys(), ",") != "a,c" || d.Len() != 2 {
		t.Errorf("keys %v", d.Keys())
	}
	if v, ok := d.Get("a"); !ok || v != 3 {
		t.Errorf("a = %v", v)
	}
	var nilDict *Dict
	if nilDict.Len() != 0 || nilDict.Keys() != nil {
		t.Error("nil dict")
	}
}

func TestTaggedKeysRoundTrip(t *testing.T) {
	// A user object with a "$type" key survives both directions.
	res := mustCall(t, read(`export default (v) => ({ echo: v, made: { $type: "element", $i: 0, other: 1 } });`, NewDict("$type", "in", "k", 1)))
	d := res.Value.(*Dict)
	echo, _ := d.Get("echo")
	made, _ := d.Get("made")
	if ed := echo.(*Dict); strings.Join(ed.Keys(), ",") != "$type,k" {
		t.Errorf("echo %v", ed.Keys())
	}
	if md := made.(*Dict); strings.Join(md.Keys(), ",") != "$type,$i,other" {
		t.Errorf("made %v", md.Keys())
	}
}

func TestShortName(t *testing.T) {
	for in, want := range map[string]string{
		"https://moodboard.pagelove.org/Note": "Note",
		"https://x.test/a/b/Thing/":           "Thing",
		"urn:Test":                            "Test",
		"https://example.org":                 "example.org",
	} {
		if got := shortName(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}
