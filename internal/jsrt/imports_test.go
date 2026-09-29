package jsrt

import (
	"errors"
	"strings"
	"testing"
)

// Imports and imported schema classes: R-JS-9, R-JS-85..87.

const noteURL = "https://moodboard.pagelove.org/Note"

func noteSchemas() []*SchemaDescriptor {
	return []*SchemaDescriptor{
		{Type: noteURL, Properties: []PropertyDescriptor{{Name: "title"}, {Name: "x"}, {Name: "y"}, {Name: "tags", Many: true}}, Methods: []MethodDescriptor{
			{Name: "label", Language: LanguageURL, Source: `export default function (prefix) { return prefix + this.title; }`},
			{Name: "kind", Static: true, Language: LanguageURL, Source: `export default function () { return "CLS:" + this.name; }`},
			{Name: "probe", Language: LanguageURL, Source: `export default function () { return typeof document + ":" + typeof Context; }`},
			{Name: "escalate", Language: "https://pagelove.org/Sessel", Source: "self.x + n"},
		}},
		{Type: "https://example.org/Sticky", Parent: noteURL, Properties: []PropertyDescriptor{{Name: "color"}}},
		{Type: "https://example.org/Cfg", Parent: MapSchemaType, Properties: []PropertyDescriptor{{Name: "name"}}},
		{Type: "urn:Test"},
	}
}

const importNote = `import Note from "https://moodboard.pagelove.org/Note" with { type: "https://pagelove.org/Schema" };` + "\n"

func TestImportNotAllowedForms(t *testing.T) {
	for name, src := range map[string]string{
		"bare":                    `import x from "lodash"; export default () => 1;`,
		"relative":                `import x from "./x.js"; export default () => 1;`,
		"http":                    `import x from "https://example.com/x.js"; export default () => 1;`,
		"pagelove:host":           `import host from "pagelove:host"; export default () => 1;`,
		"qjs:std":                 `import * as std from "qjs:std"; export default () => 1;`,
		"literal pagelove:schema": `import S from "pagelove:schema"; export default () => 1;`,
		"schema without type":     `import N from "https://moodboard.pagelove.org/Note"; export default () => 1;`,
		"wrong type":              `import N from "https://moodboard.pagelove.org/Note" with { type: "json" }; export default () => 1;`,
		"named schema import":     `import { x } from "https://moodboard.pagelove.org/Note" with { type: "https://pagelove.org/Schema" }; export default () => 1;`,
		"namespace schema import": `import * as N from "https://moodboard.pagelove.org/Note" with { type: "https://pagelove.org/Schema" }; export default () => 1;`,
		"re-export":               `export * from "https://moodboard.pagelove.org/Note"; export default () => 1;`,
		"named re-export":         `export { default as N } from "lodash"; export default () => 1;`,
		"side-effect import":      `import "lodash"; export default () => 1;`,
		"data URL":                `import x from "data:text/javascript,export default 1" with { type: "https://pagelove.org/Schema" }; export default () => 1;`,
		"dynamic import":          `export default async () => { await import("./x.js"); return 1; };`,
		"dynamic import unused":   `export default () => 1; const f = () => import("x");`,
		"unused import":           `import unused from "lodash"; export default () => 1;`,
	} {
		t.Run(name, func(t *testing.T) {
			f := expectFailure(t, CallRequest{Slot: SlotRead, Source: src, Args: []Value{nil}, Schemas: noteSchemas()}, VariantImportNotAllowed)
			if f.Message == "" {
				t.Error("no message")
			}
		})
	}
}

func TestImportUnknownSchema(t *testing.T) {
	host := &StubHost{}
	f := expectFailure(t, CallRequest{Slot: SlotRead, Args: []Value{nil}, Host: host,
		Source: `import Ghost from "https://example.org/NeverDeclared" with { type: "https://pagelove.org/Schema" }; export default () => 1;`}, VariantUnknownSchema)
	if f.Specifier != "https://example.org/NeverDeclared" {
		t.Errorf("specifier %q", f.Specifier)
	}
	if len(host.Lookups) != 1 || host.Lookups[0] != "https://example.org/NeverDeclared" {
		t.Errorf("host lookups %v", host.Lookups)
	}
}

func TestImportParseErrorWinsOverImports(t *testing.T) {
	expectFailure(t, read(`import x from "lodash"; export default (`, nil), VariantParse)
	expectFailure(t, read(`export default () => import("x"); syntax error here`, nil), VariantParse)
}

func TestDynamicImportAtRuntimeIsRejected(t *testing.T) {
	// Hidden from the pre-scan, a dynamic import still loads nothing: the
	// engine refuses import() in eval code, and the normalizer rejects every
	// import once the binding runs.
	res, f := call(t, read(`export default async () => { const s = "imp" + "ort"; return await eval(s + "('lodash')"); };`, nil))
	if f == nil {
		t.Fatalf("dynamic import loaded something: %v", res.Value)
	}
	if f.Variant != VariantImportNotAllowed && f.Variant != VariantThrew {
		t.Fatalf("got %s: %s", f.Variant, f.Message)
	}
	res = mustCall(t, read(`export default async () => { try { await eval("imp" + "ort('x')"); return "no"; } catch (e) { return e.name; } };`, nil))
	if res.Value == "no" {
		t.Errorf("caught dynamic import succeeded")
	}
}

func TestSchemaClass(t *testing.T) {
	res := mustCall(t, CallRequest{Slot: SlotRead, Args: []Value{nil}, Schemas: noteSchemas(), Source: importNote + `export default () => {
		const n = new Note({ title: "t", extra: 1 });
		const before = n.title;
		n.title = "u";
		return [Note.name, n instanceof Note, before, n.title, typeof Note, n.extra, Object.keys(n).join(","), n.label("> ")].join("|");
	};`})
	if res.Value != "Note|true|t|u|function|1|extra|> u" {
		t.Errorf("got %v", res.Value)
	}
	// The import binding name is the author's choice; the class name is the schema's.
	res = mustCall(t, CallRequest{Slot: SlotRead, Args: []Value{nil}, Schemas: noteSchemas(), Source: `import Whatever from "urn:Test" with { type: "https://pagelove.org/Schema" };
		export default () => Whatever.name;`})
	if res.Value != "Test" {
		t.Errorf("urn class name: %v", res.Value)
	}
}

func TestSchemaInheritanceAndStatic(t *testing.T) {
	res := mustCall(t, CallRequest{Slot: SlotRead, Args: []Value{nil}, Schemas: noteSchemas(), Source: importNote +
		`import Sticky from "https://example.org/Sticky" with { type: "https://pagelove.org/Schema" };
		export default () => {
			const s = new Sticky({ title: "a", color: "red" });
			return [s instanceof Sticky, s instanceof Note, s.title, s.color, s.label("L:"), Note.kind(), Sticky.name].join("|");
		};`})
	if res.Value != "true|true|a|red|L:a|CLS:Note|Sticky" {
		t.Errorf("got %v", res.Value)
	}
}

func TestMapSchema(t *testing.T) {
	res := mustCall(t, CallRequest{Slot: SlotRead, Args: []Value{nil}, Schemas: noteSchemas(), Source: `import Cfg from "https://example.org/Cfg" with { type: "https://pagelove.org/Schema" };
		export default () => {
			const c = new Cfg({ name: "n", a: "1" });
			c.set("k", "v");
			c.merge({ m: "2" });
			c.merge(new Map([["z", "3"]]));
			return [c instanceof Map, typeof c.keys, typeof c.entries, typeof c.merge, c.name, c.get("k"), c.k, c.m, c.z, [...c.keys()].join(","), c.size].join("|");
		};`})
	if res.Value != "true|function|function|function|n|v|v|2|3|k,m,z|3" {
		t.Errorf("got %v", res.Value)
	}
}

func TestReturnedInstance(t *testing.T) {
	res := mustCall(t, CallRequest{Slot: SlotDefault, This: NewDict(), Args: []Value{NewDict()}, Schemas: noteSchemas(),
		Source: importNote + `export default () => new Note({ title: "untitled", x: 0, y: 0, tags: ["a"] });`})
	inst, ok := res.Value.(*Instance)
	if !ok || inst.Type != noteURL {
		t.Fatalf("got %#v", res.Value)
	}
	if got := strings.Join(inst.Props.Keys(), ","); got != "title,x,y,tags" {
		t.Errorf("props %s", got)
	}
	if b, _ := MarshalJSON(inst); !strings.Contains(string(b), `"$type":"instance"`) {
		t.Errorf("json %s", b)
	}
}

func TestInstanceAndClassInputs(t *testing.T) {
	inst := &Instance{Type: noteURL, Props: NewDict("title", "in", "x", 3)}
	res := mustCall(t, CallRequest{Slot: SlotMethod, Schemas: noteSchemas(), This: inst, Args: []Value{"pre-"},
		Source: `export default function (p) { return [this.constructor.name, this.title, this.x, this.label(p), typeof this.escalate].join("|"); }`})
	if res.Value != "Note|in|3|pre-in|function" {
		t.Errorf("instance input: %v", res.Value)
	}
	res = mustCall(t, CallRequest{Slot: SlotMethod, Schemas: noteSchemas(), This: &Class{Type: noteURL}, Args: []Value{},
		Source: `export default function () { return "CLS:" + this.name + ":" + (new this({title: "q"})).title; }`})
	if res.Value != "CLS:Note:q" {
		t.Errorf("class input: %v", res.Value)
	}
}

func TestMethodReceiverHasNoDocument(t *testing.T) {
	res := mustCall(t, CallRequest{Slot: SlotRead, Args: []Value{nil}, Schemas: noteSchemas(), Document: &Document{HTML: "<p>x</p>"},
		Source: importNote + `export default () => [typeof document, new Note({}).probe(), typeof document].join("|");`})
	if res.Value != "object|undefined:undefined|object" {
		t.Errorf("got %v", res.Value)
	}
}

func TestSesselMethodThroughHost(t *testing.T) {
	host := &StubHost{Methods: map[string]func(*MethodCall) (*MethodResult, error){
		noteURL + "#escalate": func(c *MethodCall) (*MethodResult, error) {
			inst := c.Receiver.(*Instance)
			x, _ := inst.Props.Get("x")
			return &MethodResult{Value: NewDict("x", x, "n", c.Args[0]), ContextWrites: []ContextWrite{{Name: "fromSessel", Value: "yes"}}}, nil
		},
	}}
	res := mustCall(t, CallRequest{Slot: SlotMethod, Host: host, Schemas: noteSchemas(), This: NewDict(), Args: []Value{}, Context: NewDict(),
		Source: importNote + `export default () => { const r = new Note({x: 5}).escalate(2); return r.x + ":" + r.n + ":" + Context.fromSessel; };`})
	if res.Value != "5:2:yes" {
		t.Errorf("got %v", res.Value)
	}
	if len(host.Calls) != 1 || host.Calls[0].Method != "escalate" || res.Stats.HostCalls < 1 {
		t.Errorf("calls %v stats %+v", host.Calls, res.Stats)
	}
	// A host error is thrown into JavaScript.
	host.Methods[noteURL+"#escalate"] = func(*MethodCall) (*MethodResult, error) { return nil, errors.New("sessel failed") }
	res = mustCall(t, CallRequest{Slot: SlotMethod, Host: host, Schemas: noteSchemas(), Args: []Value{},
		Source: importNote + `export default () => { try { new Note({}).escalate(1); return "no"; } catch (e) { return e.message; } };`})
	if res.Value != "sessel failed" {
		t.Errorf("host error: %v", res.Value)
	}
}

func TestSchemaFromHostLookup(t *testing.T) {
	host := &StubHost{Schemas: map[string]*SchemaDescriptor{noteURL: noteSchemas()[0]}}
	res := mustCall(t, CallRequest{Slot: SlotRead, Args: []Value{nil}, Host: host, Source: importNote + `export default () => new Note({title: "h"}).title;`})
	if res.Value != "h" || len(host.Lookups) != 1 {
		t.Errorf("got %v lookups %v", res.Value, host.Lookups)
	}
}

func TestPrescan(t *testing.T) {
	cases := map[string]string{
		"import x from 'y'":                     "only schema imports",
		"const s = 'import x from \"y\"';":      "",
		"// import x from 'y'\n":                "",
		"/* import x from 'y' */":               "",
		"const r = /import x from 'y'/;":        "",
		"const t = `import ${a} from 'y'`;":     "",
		"const t = `${ `import x from 'y'` }`;": "",
		"a.import('x')":                         "",
		"import.meta.url":                       "",
		"import('x')":                           "dynamic",
		"export * from 'x'":                     "re-export",
		"export { a } from 'x'":                 "re-export",
		"export { a }; const b = 1":             "",
		"x = a / b; import y from 'z'":          "only schema imports",
		"import N from 'u:v' with { type: 'https://pagelove.org/Schema' }": "",
	}
	for src, want := range cases {
		p := prescan(src)
		if want == "" && p.Problem != "" || want != "" && !strings.Contains(p.Problem, want) {
			t.Errorf("%q: problem %q, want %q", src, p.Problem, want)
		}
	}
}
