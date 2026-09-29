package schema

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

// Method lookup and dispatch for method elements (composition area,
// R-MOD-58/59, R-SESSEL-296, R-JS-18/19). Composition resolves an element
// `<ns:name …>` (element form) or an attribute `ns:name="…"` (attribute
// form) whose namespace URI is a schema type URL, then calls Dispatch; it
// splices the returned value into the page.

// MethodCall describes one dispatch.
type MethodCall struct {
	Site *site.Site
	Snap *site.Snapshot
	// Type is the schema type URL (the namespace URI of the method
	// element); Name the local name (lower-case as parsed).
	Type string
	Name string
	// Host is the dispatching element (element form) or the element carrying
	// the attribute (attribute form); it is `self` / `this`, read-only.
	Host *html.Node
	// Path is the document being composed (provenance of Host).
	Path string
	// AttributeForm marks the attribute form; Value is then the attribute
	// value, bound to the first declared parameter.
	AttributeForm bool
	Value         string
	// Env carries the composition pass's Context, request and budget; nil
	// gets a fresh Context and the request budget from ctx.
	Env *sessel.Env
}

// ErrNoMethod reports a dispatch that no method (nor doesNotUnderstand)
// handles; composition leaves such an element unchanged.
var ErrNoMethod = fmt.Errorf("schema: no such method")

// FindMethod returns the method a dispatch selects (R-MOD-59, R-COMP-61):
// among the most-derived schema declaring the name, the overload whose
// declared parameters are all supplied as attributes, the largest full
// match winning. When no overload is fully supplied, the first one declared
// is called with the missing parameters null (Method-Elements "an absent
// attribute is null"; case javascript.methods.absent-parameter-is-null);
// nil when no schema of the chain declares the name. Static methods are
// included.
func (r *Registry) FindMethod(typeURL, name string, attrs []html.Attribute) *MethodDecl {
	have := map[string]bool{}
	for _, a := range attrs {
		have[strings.ToLower(a.Key)] = true
	}
	for _, s := range r.Chain(typeURL) {
		var best, first *MethodDecl
		for _, m := range s.Methods {
			if !strings.EqualFold(m.Name, name) {
				continue
			}
			if first == nil {
				first = m
			}
			all := true
			for _, p := range m.Params {
				if !have[strings.ToLower(p)] {
					all = false
					break
				}
			}
			if all && (best == nil || len(m.Params) > len(best.Params)) {
				best = m
			}
		}
		if best != nil {
			return best
		}
		if first != nil {
			return first
		}
	}
	return nil
}

// DispatchResult is the outcome of DispatchMethod.
type DispatchResult struct {
	Value sessel.Value
	// Method is the declaration that ran (doesNotUnderstand for the
	// fallback); its Returns tells composition whether an attribute-form
	// result replaces the host (R-COMP-63).
	Method *MethodDecl
	// Private reports that a JavaScript implementation read a
	// per-requester member of request (R-JS-34).
	Private bool
}

// Dispatch runs the method a method element selects, or the schema's
// doesNotUnderstand (with messageName and parameters) when none matches.
// Sessel bodies see the declared parameters as named locals bound from
// the host's attributes (null when absent; R-SESSEL-296); JavaScript bodies
// get them positionally, in declared order (R-JS-18). It returns
// ErrNoMethod when nothing handles the call.
func Dispatch(ctx context.Context, mc *MethodCall) (sessel.Value, error) {
	res, err := DispatchMethod(ctx, mc)
	if err != nil {
		return nil, err
	}
	return res.Value, nil
}

// DispatchMethod is Dispatch reporting which declaration ran. A JavaScript
// body runs with the host element as `this` and the read-only `document`
// scoped to it (R-JS-18); its Context assignments are written to
// mc.Env.Context.
func DispatchMethod(ctx context.Context, mc *MethodCall) (*DispatchResult, error) {
	reg := For(mc.Snap)
	if reg.Schema(mc.Type) == nil {
		return nil, ErrNoMethod
	}
	var attrs []html.Attribute
	if mc.Host != nil && !mc.AttributeForm {
		attrs = mc.Host.Attr
	}
	m := reg.FindMethod(mc.Type, mc.Name, attrs)
	if mc.AttributeForm {
		m = nil
		for _, s := range reg.Chain(mc.Type) {
			for _, x := range s.Methods {
				if strings.EqualFold(x.Name, mc.Name) && (m == nil || len(x.Params) > 0 && len(m.Params) == 0) {
					m = x
				}
			}
			if m != nil {
				break
			}
		}
	}
	args := map[string]sessel.Value{}
	var positional []any
	if m != nil {
		for i, p := range m.Params {
			var v sessel.Value
			switch {
			case mc.AttributeForm && i == 0:
				v = mc.Value
			case !mc.AttributeForm && mc.Host != nil:
				if s, ok := attrFold(mc.Host, p); ok {
					v = s
				}
			}
			args[p] = v
			positional = append(positional, sessel.ToGo(v))
		}
	} else {
		dnu := reg.FindMethod(mc.Type, "doesNotUnderstand", nil)
		if dnu == nil {
			for _, s := range reg.Chain(mc.Type) {
				for _, x := range s.Methods {
					if x.Name == "doesNotUnderstand" {
						dnu = x
						break
					}
				}
				if dnu != nil {
					break
				}
			}
		}
		if dnu == nil {
			return nil, ErrNoMethod
		}
		m = dnu
		params := sessel.List{}
		if mc.AttributeForm {
			params = append(params, mc.Value)
		} else if mc.Host != nil {
			for _, a := range mc.Host.Attr {
				if strings.HasPrefix(a.Key, "xmlns:") {
					continue
				}
				d := sessel.NewDict()
				d.Set("name", a.Key)
				d.Set("value", a.Val)
				params = append(params, d)
			}
		}
		args["messageName"] = mc.Name
		args["parameters"] = params
		positional = []any{mc.Name, sessel.ToGo(params)}
	}
	env := &sessel.Env{}
	if mc.Env != nil {
		cp := *mc.Env
		env = &cp
	}
	ev := newEvaluator(ctx, mc.Site, mc.Snap, reg)
	if env.Budget == nil {
		env.Budget = ev.budget
	}
	if env.Host == nil {
		env.Host = ev.host
	}
	var self *sessel.Element
	if mc.Host != nil {
		doc := &sessel.Document{Path: mc.Path, Root: documentOf(mc.Host)}
		self = queried(mc.Host, doc)
		if c := reg.Class(mc.Type); c != nil {
			self.Class = c
		}
	}
	out := &DispatchResult{Method: m}
	impl := m.Implementation
	if impl == nil || impl.Lang == LangSessel && strings.TrimSpace(impl.Source) == "" {
		// An absent or empty Sessel implementation yields null, which
		// removes the element (R-COMP-62); an empty JavaScript module is a
		// shape failure (R-JS-1).
		return out, nil
	}
	switch impl.Lang {
	case LangSessel:
		if err := impl.compile(); err != nil {
			return nil, err
		}
		env.Self, env.HasSelf = self, self != nil
		vars := map[string]sessel.Value{}
		for k, v := range env.Vars {
			vars[k] = v
		}
		for k, v := range args {
			vars[k] = v
		}
		env.Vars = vars
		v, err := impl.compiled.Eval(ctx, env)
		if err != nil {
			return nil, err
		}
		out.Value = v
		return out, nil
	case LangJS:
		call := &JSCall{Slot: JSSlotMethod, Source: impl.Source, Args: positional, Context: env.Context, Classes: reg,
			Host: env.Host, Budget: env.Budget}
		if mc.Host != nil {
			call.This, call.HasThis = sessel.ToGo(self), true
			call.Document = dom.OuterHTML(mc.Host)
			call.HostNode, call.Path = mc.Host, mc.Path
		}
		res, err := runJS(ctx, call)
		if err != nil {
			return nil, err
		}
		out.Value, out.Private = fromJS(res.Value), res.Private
		return out, nil
	}
	return nil, unknownLanguage(impl)
}

func attrFold(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val, true
		}
	}
	return "", false
}

// KeyID returns the value Pagelove.PUT writes as the id of a materialised
// instance (R-MOD-34): the honoured @key property's value, when it is a
// single non-empty value without whitespace and the property is not
// computed. The writer (reactions area) sets it on the root element before
// storing; plain HTTP bodies are stored as sent.
func KeyID(snap *site.Snapshot, inst *html.Node) (string, bool) {
	if inst == nil || !isItem(inst) {
		return "", false
	}
	key := For(snap).KeyProperty(itemType(inst))
	if key == nil || key.Decl.Computed != nil {
		return "", false
	}
	vals := valueStrings(propElements(inst, key.Name))
	if len(vals) != 1 || vals[0] == "" || strings.ContainsAny(vals[0], " \t\n\r\f") {
		return "", false
	}
	return vals[0], true
}

// SetKeyID applies KeyID to inst in place, overwriting any id.
func SetKeyID(snap *site.Snapshot, inst *html.Node) {
	if id, ok := KeyID(snap, inst); ok {
		dom.SetAttr(inst, "id", id)
	}
}
