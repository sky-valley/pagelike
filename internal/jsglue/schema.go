package jsglue

import (
	"context"

	"github.com/sky-valley/pagelike/internal/jsrt"
	"github.com/sky-valley/pagelike/internal/schema"
	"github.com/sky-valley/pagelike/internal/sessel"
)

// schemaRunner implements schema.JSRunner (defaults, resolvers,
// validators, computed properties, methods, Group includes()).
type schemaRunner struct{}

var schemaSlots = map[string]jsrt.Slot{
	schema.JSSlotDefault:        jsrt.SlotDefault,
	schema.JSSlotRead:           jsrt.SlotRead,
	schema.JSSlotWrite:          jsrt.SlotWrite,
	schema.JSSlotValidate:       jsrt.SlotValidate,
	schema.JSSlotSchemaValidate: jsrt.SlotSchemaValidate,
	schema.JSSlotComputed:       jsrt.SlotComputed,
	schema.JSSlotMethod:         jsrt.SlotMethod,
}

// Run implements schema.JSRunner.
func (schemaRunner) Run(ctx context.Context, call *schema.JSCall) (*schema.JSResult, error) {
	slot, ok := schemaSlots[call.Slot]
	if !ok {
		return nil, &schema.BindingFailure{Language: jsrt.LanguageURL, Variant: string(jsrt.VariantInternal), Message: "unknown JavaScript slot " + call.Slot}
	}
	h := &host{reg: call.Classes, sessel: call.Host, budget: call.Budget, cx: call.Context}
	req := jsrt.CallRequest{Source: call.Source, Slot: slot, Host: h}
	switch {
	case call.HostNode != nil:
		// Composition dispatch (R-JS-18): this is the dispatching element and
		// the read-only document is scoped to its subtree.
		req.Slot = jsrt.SlotDispatch
		req.This = &jsrt.Element{Node: call.HostNode, Source: call.Path}
		req.Document = &jsrt.Document{Node: call.HostNode, Source: call.Path}
	case call.Receiver != nil:
		this, doc := receiver(call.Receiver, call.Classes)
		req.This, req.Document = this, doc
	case call.HasThis:
		if req.This = fromGo(call.This); req.This == nil {
			req.This = jsrt.Null
		}
	}
	for _, a := range call.Args {
		req.Args = append(req.Args, fromGo(a))
	}
	if req.Document == nil && call.Document != "" {
		if call.DocumentWritable {
			// The default slot's writable view of the in-progress instance:
			// documentElement is the instance element (R-JS-11).
			if n := parseElement(call.Document); n != nil {
				req.Document = &jsrt.Document{Node: n, Source: call.Path}
			}
		} else {
			req.Document = &jsrt.Document{HTML: call.Document, Source: call.Path}
		}
	}
	if slot == jsrt.SlotMethod || req.Slot == jsrt.SlotDispatch {
		req.Context = contextValue(call.Context, call.Classes)
	}
	res, f := callModule(ctx, req)
	if f != nil {
		return nil, schemaFailure(f)
	}
	out := &schema.JSResult{Value: toGo(res.Value), Private: res.Tainted}
	if res.DocumentChanged {
		out.Document = res.Document
	}
	return out, nil
}

// receiver converts the Sessel receiver of a direct method call (R-JS-17):
// a class for a static method, an instance of an imported class for an
// element of a registered schema (no document), any other element as a
// read-only element that is also the root of the document.
func receiver(v sessel.Value, reg *schema.Registry) (jsrt.Value, *jsrt.Document) {
	switch x := v.(type) {
	case sessel.Class:
		return &jsrt.Class{Type: x.URL()}, nil
	case *sessel.Element:
		if x == nil || x.Node == nil {
			return jsrt.Null, nil
		}
		jv := fromSesselLenient(x, reg)
		if el, ok := jv.(*jsrt.Element); ok && el.Node != nil {
			return el, &jsrt.Document{Node: el.Node, Source: el.Source}
		}
		return jv, nil
	}
	jv := fromSesselLenient(v, reg)
	if jv == nil {
		return jsrt.Null, nil
	}
	return jv, nil
}

// schemaFailure maps a jsrt failure onto the schema error model: a thrown
// HTTPResponse the slot honours (R-JS-53), else a BindingFailure (R-JS-50).
func schemaFailure(f *jsrt.Failure) error {
	if f.Response != nil {
		return &schema.JSThrownResponse{Status: f.Response.Status, Message: f.Response.Message, Body: f.Response.Body, HasBody: true, Headers: f.Response.Headers}
	}
	return &schema.BindingFailure{Language: jsrt.LanguageURL, Variant: string(f.Variant), Message: f.Message, Stack: f.Stack}
}
