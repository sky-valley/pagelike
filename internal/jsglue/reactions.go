package jsglue

import (
	"context"
	"strings"

	"github.com/sky-valley/pagelike/internal/budget"
	"github.com/sky-valley/pagelike/internal/jsrt"
	"github.com/sky-valley/pagelike/internal/reactions"
	"github.com/sky-valley/pagelike/internal/schema"
)

// reactionsRunner implements reactions.JSRunner: trigger and processor
// gates and actions, dynamic HttpRequest properties and j: bindings of
// reactions (R-JS-21..24).
type reactionsRunner struct{}

var reactionSlots = map[string]jsrt.Slot{
	reactions.SlotTriggerWhen:         jsrt.SlotTriggerWhen,
	reactions.SlotTriggerAction:       jsrt.SlotTriggerAction,
	reactions.SlotTriggerOtherwise:    jsrt.SlotTriggerOtherwise,
	reactions.SlotProcessorWhen:       jsrt.SlotProcessorWhen,
	reactions.SlotProcessorAction:     jsrt.SlotProcessorAction,
	reactions.SlotHTTPRequestProperty: jsrt.SlotHTTPRequestProperty,
	// A j: binding is an expression: a thrown HTTPResponse is an ordinary
	// failure there, as in a gate.
	reactions.SlotBinding: jsrt.SlotTriggerWhen,
}

// CallDefault implements reactions.JSRunner.
func (reactionsRunner) CallDefault(ctx context.Context, call *reactions.JSCall) (any, error) {
	slot, ok := reactionSlots[call.Slot]
	if !ok {
		slot = jsrt.SlotTriggerAction
	}
	var reg *schema.Registry
	if call.Snap != nil {
		reg = schema.For(call.Snap)
	}
	req := jsrt.CallRequest{Source: call.Source, Slot: slot, Host: &host{reg: reg}, Args: []jsrt.Value{reactionContext(call.Arg)}}
	if call.Budget != nil {
		ctx = budget.With(ctx, call.Budget)
	}
	res, f := callModule(ctx, req)
	if f != nil {
		if r := f.Response; r != nil {
			return nil, &reactions.ThrownResponse{Status: r.Status, Message: r.Message, HasMessage: r.Message != "", Body: r.Body, HasBody: true, Headers: r.Headers}
		}
		return nil, &reactions.JSError{Variant: string(f.Variant), Message: f.Message, Stack: f.Stack}
	}
	return toGo(res.Value), nil
}

// reactionContext is the JavaScript ctx (R-JS-21/22): request (and response
// for processors) with header objects that look names up case-insensitively
// (R-JS-24). An anonymous request's auth is {claims: {}, roles: []}, as on
// PageLove (live 2026-09-29, decisions-2026-09-29/reacting.md); a nil auth
// is left out.
func reactionContext(arg map[string]any) *jsrt.Dict {
	d := jsrt.NewDict()
	for _, k := range []string{"request", "response"} {
		if m, ok := arg[k].(map[string]any); ok {
			d.Set(k, section(m))
		}
	}
	for _, k := range sortedKeys(arg) {
		if k != "request" && k != "response" {
			d.Set(k, fromGo(arg[k]))
		}
	}
	return d
}

var sectionOrder = []string{"method", "path", "host", "headers", "query", "body", "auth", "status"}

func section(m map[string]any) *jsrt.Dict {
	d := jsrt.NewDict()
	put := func(k string) {
		v, ok := m[k]
		if !ok {
			return
		}
		switch {
		case k == "auth" && v == nil:
			return // no auth given: request.auth is undefined
		case k == "headers":
			if hm, ok := v.(map[string]any); ok {
				h := &jsrt.Headers{}
				for _, name := range sortedKeys(hm) {
					if s, ok := hm[name].(string); ok {
						h.Add(strings.ToLower(name), s)
					}
				}
				d.Set(k, h)
				return
			}
		}
		d.Set(k, fromGo(v))
	}
	seen := map[string]bool{}
	for _, k := range sectionOrder {
		seen[k] = true
		put(k)
	}
	for _, k := range sortedKeys(m) {
		if !seen[k] {
			put(k)
		}
	}
	return d
}
