package compose

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/microdata"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

// Method elements and attributes (docs/spec/composing.md §10) dispatch to
// the methods of the schema whose type URI a prefix is bound to. The schema
// package owns declarations (area modeling R-MOD-58..60); composition only
// invokes them, through a MethodDispatcher. SchemaMethods, the default,
// reads https://pagelove.org/Schema items straight from the site snapshot
// (like sessel.MicrodataClasses does for classes) and runs Sessel
// implementations in-process and JavaScript ones through the JSRunner. The
// schema package may install a richer dispatcher with SetMethodDispatcher.

// URL of the Element return type (R-COMP-63).
const URLElement = "https://pagelove.org/Element"

// ErrNoSchema reports a type URI no registered schema declares.
var ErrNoSchema = errors.New("compose: no schema declares this type")

// ErrNoMethod reports a schema without the method and without
// doesNotUnderstand.
var ErrNoMethod = errors.New("compose: no method found and no doesNotUnderstand defined")

// MethodCall is one dispatch of a method element or attribute.
type MethodCall struct {
	Snap    *site.Snapshot
	TypeURL string
	// Name is the element's or attribute's local name (the message name).
	Name string
	// Attribute selects the attribute form: Value is the attribute value.
	Attribute bool
	Value     string
	// Attrs are the element form's attributes (in source order).
	Attrs []html.Attribute
	// Self is the dispatched element (`self` / `this`).
	Self    *sessel.Element
	DocPath string
	// Context holds the visible Context names and request; implementations
	// may add or change entries, which composition makes visible to the
	// dispatched element's subtree (R-COMP-21).
	Context *sessel.Dict
	Request *sessel.Dict
	Host    sessel.Host
	Budget  *sessel.Budget
	JS      JSRunner
}

// MethodResult is a dispatch outcome.
type MethodResult struct {
	Value sessel.Value
	// ReturnsElement: the method declares returns https://pagelove.org/Element.
	ReturnsElement bool
	// Private: the implementation read per-requester request members.
	Private bool
	// Context entries set by a JavaScript implementation.
	Context map[string]sessel.Value
}

// MethodDispatcher resolves and invokes schema methods.
type MethodDispatcher interface {
	// HasSchema reports whether typeURL is a registered schema type.
	HasSchema(snap *site.Snapshot, typeURL string) bool
	// Dispatch invokes the method named call.Name (or doesNotUnderstand).
	// It returns ErrNoSchema or ErrNoMethod (possibly wrapped) when there is
	// nothing to call.
	Dispatch(ctx context.Context, call *MethodCall) (*MethodResult, error)
}

// SchemaMethods is the default MethodDispatcher (see the file comment).
type SchemaMethods struct{}

type schemaDecl struct {
	typeURL, parent string
	methods         []*methodDecl
}

type methodDecl struct {
	name    string
	params  []string
	returns string
	lang    string // "sessel", "javascript" or "" (no implementation)
	source  string
}

const schemasKey = "compose.schemas"

func schemasOf(snap *site.Snapshot) map[string]*schemaDecl {
	return snap.Ext(schemasKey, func(s *site.Snapshot) any {
		out := map[string]*schemaDecl{}
		for _, ti := range s.ItemsOfType(sessel.URLSchema) {
			it := ti.Item
			t := strings.TrimSpace(it.Get("type"))
			if t == "" {
				continue
			}
			if _, dup := out[t]; dup {
				continue // the first declaration wins
			}
			d := &schemaDecl{typeURL: t, parent: strings.TrimSpace(it.Get("parent"))}
			for _, p := range it.Props {
				if (p.Name != "property" && p.Name != "method") || p.Item == nil || !p.Item.HasType(sessel.URLMethod) {
					continue
				}
				if m := methodFrom(p.Item); m != nil {
					d.methods = append(d.methods, m)
				}
			}
			out[t] = d
		}
		return out
	}).(map[string]*schemaDecl)
}

func methodFrom(it *microdata.Item) *methodDecl {
	name := strings.TrimSpace(it.Get("name"))
	if name == "" {
		return nil
	}
	m := &methodDecl{name: name, returns: strings.TrimSpace(it.Get("returns"))}
	for _, pi := range it.Items("parameter") {
		if pn := strings.TrimSpace(pi.Get("name")); pn != "" {
			m.params = append(m.params, pn)
		}
	}
	for _, p := range it.Props {
		if p.Name != "implementation" {
			continue
		}
		if p.Item != nil {
			m.source = strings.TrimSpace(p.Item.Get("source"))
			for _, t := range p.Item.Types {
				switch {
				case t == sessel.URLSessel || t == sessel.URLLambda:
					m.lang = "sessel"
				case strings.Contains(t, "JavaScript"):
					m.lang = "javascript"
				}
			}
		} else if p.Node != nil && p.Node.Data == "script" {
			m.source = strings.TrimSpace(p.Value)
			if t := strings.ToLower(strings.TrimSpace(attr(p.Node, "type"))); t == "text/sessel" {
				m.lang = "sessel"
			} else {
				m.lang = "javascript"
			}
		}
		break
	}
	return m
}

// HasSchema implements MethodDispatcher.
func (SchemaMethods) HasSchema(snap *site.Snapshot, typeURL string) bool {
	_, ok := schemasOf(snap)[typeURL]
	return ok
}

// find looks name up along the schema's parent chain; for the element
// form, among overloads the one whose parameters all appear as attributes
// and that declares the most parameters wins (R-COMP-61).
func find(decls map[string]*schemaDecl, typeURL, name string, attrs map[string]bool) *methodDecl {
	seen := map[string]bool{}
	for t := typeURL; t != "" && !seen[t]; {
		seen[t] = true
		d := decls[t]
		if d == nil {
			return nil
		}
		var first, best *methodDecl
		for _, m := range d.methods {
			if m.name != name {
				continue
			}
			if first == nil {
				first = m
			}
			if attrs == nil {
				break
			}
			all := true
			for _, p := range m.params {
				all = all && attrs[strings.ToLower(p)] // HTML lower-cases attribute names
			}
			if all && (best == nil || len(m.params) > len(best.params)) {
				best = m
			}
		}
		if best != nil {
			return best
		}
		if first != nil {
			return first
		}
		t = d.parent
	}
	return nil
}

// Dispatch implements MethodDispatcher.
func (SchemaMethods) Dispatch(ctx context.Context, call *MethodCall) (*MethodResult, error) {
	decls := schemasOf(call.Snap)
	if decls[call.TypeURL] == nil {
		return nil, ErrNoSchema
	}
	var present map[string]bool
	if !call.Attribute {
		present = map[string]bool{}
		for _, a := range call.Attrs {
			present[strings.ToLower(a.Key)] = true
		}
	}
	m := find(decls, call.TypeURL, call.Name, present)
	dnu := false
	if m == nil {
		if m = find(decls, call.TypeURL, "doesNotUnderstand", nil); m == nil {
			return nil, ErrNoMethod
		}
		dnu = true
	}
	vars := map[string]sessel.Value{}
	var args []sessel.Value
	switch {
	case dnu:
		params := sessel.List{}
		if call.Attribute {
			params = append(params, call.Value)
		} else {
			for _, a := range call.Attrs {
				if strings.HasPrefix(a.Key, "xmlns:") {
					continue
				}
				d := sessel.NewDict()
				d.Set("name", a.Key)
				d.Set("value", a.Val)
				params = append(params, d)
			}
		}
		vars["messageName"], vars["parameters"] = call.Name, params
		args = []sessel.Value{call.Name, params}
	case call.Attribute:
		for i, p := range m.params {
			var v sessel.Value
			if i == 0 {
				v = call.Value
			}
			vars[p] = v
			args = append(args, v)
		}
	default:
		for _, p := range m.params {
			var v sessel.Value
			for _, a := range call.Attrs {
				if a.Key == p || strings.EqualFold(a.Key, p) {
					v = a.Val
					break
				}
			}
			vars[p] = v
			args = append(args, v)
		}
	}
	res := &MethodResult{ReturnsElement: m.returns == URLElement}
	switch m.lang {
	case "sessel":
		if m.source == "" {
			return res, nil // an empty implementation removes the element
		}
		env := &sessel.Env{Host: call.Host, Self: call.Self, HasSelf: call.Self != nil, Context: call.Context,
			Request: call.Request, Vars: vars, Budget: call.Budget}
		v, err := sessel.Eval(ctx, m.source, env)
		if err != nil {
			return nil, sesselError("method "+m.name, err)
		}
		res.Value, res.Private = v, readsIdentity(m.source)
	case "javascript":
		if call.JS == nil {
			return nil, errNoJS("the JavaScript method " + m.name)
		}
		r, err := call.JS.Method(ctx, &JSMethod{Snap: call.Snap, TypeURL: call.TypeURL, Name: m.name, Source: m.source, DocPath: call.DocPath,
			Self: call.Self, Args: args, Context: call.Context, Request: call.Request})
		if err != nil {
			return nil, wrapError("method "+m.name, err)
		}
		res.Value, res.Private, res.Context = r.Value, r.Private, r.Context
	}
	return res, nil
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}
