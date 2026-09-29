package jsglue

import (
	"context"
	"fmt"
	"strings"

	"github.com/sky-valley/pagelike/internal/jsrt"
	"github.com/sky-valley/pagelike/internal/schema"
	"github.com/sky-valley/pagelike/internal/sessel"
)

// host serves the coarse callbacks of one evaluation (jsrt.Host).
type host struct {
	// reg is the schema registry of the request's snapshot (nil: no schema
	// is known, imports fail with unknown-schema).
	reg *schema.Registry
	// sessel and budget run Sessel method bodies called from JavaScript.
	sessel sessel.Host
	budget *sessel.Budget
	// cx receives Context assignments (nil: they are only reported in the
	// result).
	cx *sessel.Dict
}

// LookupSchema implements jsrt.Host: the descriptor of a registered schema
// (R-JS-85): its parent, its own declared properties and methods (inherited
// members come from the parent's class).
func (h *host) LookupSchema(_ context.Context, typeURL string) (*jsrt.SchemaDescriptor, error) {
	if h.reg == nil {
		return nil, nil
	}
	s := h.reg.Schema(typeURL)
	if s == nil {
		return nil, nil
	}
	return descriptor(s), nil
}

func descriptor(s *schema.Schema) *jsrt.SchemaDescriptor {
	d := &jsrt.SchemaDescriptor{Type: s.URL, Parent: s.ParentURL}
	for _, p := range s.Props {
		d.Properties = append(d.Properties, jsrt.PropertyDescriptor{Name: p.Name, Many: isMulti(p.Cardinality)})
	}
	for _, m := range s.Methods {
		md := jsrt.MethodDescriptor{Name: m.Name, Static: m.Static, Params: append([]string(nil), m.Params...)}
		if impl := m.Implementation; impl != nil {
			md.Language, md.Source = impl.LanguageURL(), impl.Source
		}
		d.Methods = append(d.Methods, md)
	}
	return d
}

// CallMethod implements jsrt.Host: a method of an imported class whose body
// is not JavaScript (a Sessel body) runs here, with the receiver as self and
// the declared parameters as named locals (R-JS-17); its Context changes
// are reported back to the JavaScript Context.
func (h *host) CallMethod(ctx context.Context, call *jsrt.MethodCall) (*jsrt.MethodResult, error) {
	if h.reg == nil {
		return nil, fmt.Errorf("TypeError: %s is not a method of a registered schema", call.Method)
	}
	var m *schema.MethodDecl
	for _, s := range h.reg.Chain(call.Type) {
		for _, x := range s.Methods {
			if x.Name == call.Method && x.Static == call.Static && (m == nil || len(x.Params) == len(call.Args)) {
				m = x
			}
		}
		if m != nil {
			break
		}
	}
	if m == nil || m.Implementation == nil {
		return nil, fmt.Errorf("TypeError: %s has no method %s", call.Type, call.Method)
	}
	impl := m.Implementation
	if impl.Lang != schema.LangSessel {
		return nil, fmt.Errorf("TypeError: method %s is implemented in %s, which cannot be called from JavaScript", call.Method, impl.LanguageURL())
	}
	vars := map[string]sessel.Value{}
	for i, p := range m.Params {
		var v sessel.Value
		if i < len(call.Args) {
			v = toSessel(call.Args[i], h.reg)
		}
		vars[p] = v
	}
	cx := h.cx
	if cx == nil {
		cx = sessel.NewContext(nil)
	}
	before := snapshotDict(cx)
	sh := h.sessel
	if sh == nil {
		sh = sessel.NewMemHost()
	}
	bud := h.budget
	if bud == nil {
		bud = sessel.NewBudget()
	}
	env := &sessel.Env{Host: sh, Self: toSessel(call.Receiver, h.reg), HasSelf: true, Vars: vars, Context: cx, Budget: bud}
	if strings.TrimSpace(impl.Source) == "" {
		return &jsrt.MethodResult{}, nil
	}
	v, err := sessel.Eval(ctx, impl.Source, env)
	if err != nil {
		return nil, err
	}
	jv, err := fromSessel(v, h.reg)
	if err != nil {
		return nil, fmt.Errorf("TypeError: the result of %s cannot be passed to JavaScript: %v", call.Method, err)
	}
	res := &jsrt.MethodResult{Value: jv}
	for _, w := range diffDict(before, cx) {
		if w.Name == "request" {
			continue
		}
		if !w.Deleted {
			if w.Value, err = fromSessel(w.sv, h.reg); err != nil {
				continue // no JavaScript form: invisible to the caller
			}
		}
		res.ContextWrites = append(res.ContextWrites, w.ContextWrite)
	}
	return res, nil
}

// WriteContext implements jsrt.Host: a Context assignment is written back to
// the calling pass's Context (R-JS-17).
func (h *host) WriteContext(_ context.Context, w jsrt.ContextWrite) error {
	if h.cx == nil || w.Name == "request" {
		return nil
	}
	if w.Deleted {
		h.cx.Delete(w.Name)
		return nil
	}
	h.cx.Set(w.Name, toSessel(w.Value, h.reg))
	return nil
}

func snapshotDict(d *sessel.Dict) map[string]sessel.Value {
	out := map[string]sessel.Value{}
	for _, k := range d.Keys() {
		out[k] = d.Lookup(k)
	}
	return out
}

type contextChange struct {
	jsrt.ContextWrite
	sv sessel.Value
}

// diffDict lists the entries of d that differ from before.
func diffDict(before map[string]sessel.Value, d *sessel.Dict) []contextChange {
	var out []contextChange
	now := map[string]bool{}
	for _, k := range d.Keys() {
		now[k] = true
		v := d.Lookup(k)
		if old, ok := before[k]; ok && sessel.Equal(old, v) {
			continue
		}
		out = append(out, contextChange{ContextWrite: jsrt.ContextWrite{Name: k}, sv: v})
	}
	for k := range before {
		if !now[k] {
			out = append(out, contextChange{ContextWrite: jsrt.ContextWrite{Name: k, Deleted: true}})
		}
	}
	return out
}

// contextValue is the JavaScript Context of a pass: every entry that has a
// JavaScript form, request as the request object (R-JS-17, R-JS-34).
func contextValue(cx *sessel.Dict, reg *schema.Registry) *jsrt.Dict {
	d := jsrt.NewDict()
	if cx == nil {
		return d
	}
	for _, k := range cx.Keys() {
		v := cx.Lookup(k)
		if k == "request" {
			if rd, ok := v.(*sessel.Dict); ok {
				d.Set(k, requestOf(rd))
				continue
			}
		}
		jv, err := fromSessel(v, reg)
		if err != nil {
			continue // no JavaScript form: the entry is invisible
		}
		d.Set(k, jv)
	}
	return d
}

// requestOf converts a Sessel request object (sessel.NewRequest) to the
// JavaScript request (R-JS-34): method, path, query, params and body are
// shared members; headers and auth are per-user members that taint the
// response when read. auth is undefined for anonymous requests.
func requestOf(rd *sessel.Dict) *jsrt.Request {
	if rd == nil {
		return nil
	}
	r := &jsrt.Request{Method: sessel.TextOf(rd.Lookup("method")), Path: sessel.TextOf(rd.Lookup("path"))}
	r.Query = stringDict(rd.Lookup("query"))
	r.Params = stringDict(rd.Lookup("params"))
	if b, ok := rd.Lookup("body").(string); ok && b != "" {
		r.Body = b
	}
	hs := &jsrt.Headers{}
	if hd, ok := rd.Lookup("headers").(*sessel.Dict); ok {
		for _, k := range hd.Keys() {
			hs.Add(k, sessel.TextOf(hd.Lookup(k)))
		}
	}
	r.Headers = hs
	if ad, ok := rd.Lookup("auth").(*sessel.Dict); ok && ad.Lookup("username") != nil {
		a := jsrt.NewDict()
		a.Set("claims", fromSesselLenient(ad.Lookup("claims"), nil))
		a.Set("username", fromSesselLenient(ad.Lookup("username"), nil))
		roles := fromSesselLenient(ad.Lookup("roles"), nil)
		a.Set("role", roles)
		a.Set("roles", roles)
		r.Auth = a
	}
	return r
}

func stringDict(v sessel.Value) *jsrt.Dict {
	d := jsrt.NewDict()
	if sd, ok := v.(*sessel.Dict); ok {
		for _, k := range sd.Keys() {
			d.Set(k, fromSesselLenient(sd.Lookup(k), nil))
		}
	}
	return d
}
