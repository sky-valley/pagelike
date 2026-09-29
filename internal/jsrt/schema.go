package jsrt

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Schema imports resolve to generated modules (ADR 0001 §Schema imports):
// a class named after the schema, extending its parent's class (or Map),
// with accessor pairs for the declared properties over a Symbol-keyed store
// and methods whose JavaScript bodies are separate modules and whose other
// bodies call back to the host (R-JS-85/86).

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// methodModuleName names the module holding a JavaScript method body; it is
// also the file name in that method's stack frames.
func methodModuleName(d *SchemaDescriptor, m MethodDescriptor) string {
	if m.Static {
		return d.Type + "#static." + m.Name
	}
	return d.Type + "#" + m.Name
}

func isJSMethod(m MethodDescriptor) bool {
	return m.Language == LanguageURL || m.Language == "" && m.Source != ""
}

// classModuleSource generates the module for schema d. parentOK says the
// parent is a resolved schema (imported); isMap that the chain reaches the
// Map schema directly (the class extends Map).
func classModuleSource(d *SchemaDescriptor, parentOK, isMap bool) string {
	var b strings.Builder
	parent := "null"
	if parentOK {
		b.WriteString("import __Parent from " + jsString(d.Parent) + ";\n")
		parent = "__Parent"
	}
	var methods []string
	for i, m := range d.Methods {
		js := "null"
		if isJSMethod(m) {
			local := fmt.Sprintf("__m%d", i)
			b.WriteString("import " + local + " from " + jsString(methodModuleName(d, m)) + ";\n")
			js = local
		}
		methods = append(methods, fmt.Sprintf("{name:%s,static:%v,js:%s}", jsString(m.Name), m.Static, js))
	}
	props := make([]string, len(d.Properties))
	for i, p := range d.Properties {
		props[i] = jsString(p.Name)
	}
	fmt.Fprintf(&b, "export default __pagelike.schema.defineClass({type:%s,name:%s,parent:%s,map:%v,props:[%s],methods:[%s]});\n",
		jsString(d.Type), jsString(d.className()), parent, isMap && !parentOK, strings.Join(props, ","), strings.Join(methods, ","))
	return b.String()
}

// entryModuleSource is the module the driver imports: value classes first
// (so they exist before the binding's module runs and Context values can be
// decoded), then the binding's module.
func entryModuleSource(valueTypes []string) string {
	var b strings.Builder
	for i, t := range valueTypes {
		fmt.Fprintf(&b, "import __c%d from %s;\n", i, jsString(t))
	}
	b.WriteString("import * as __main from \"eval\";\nexport { __main as main };\n")
	return b.String()
}
