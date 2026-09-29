package jsrt

import "fmt"

// Slot is where a JavaScript binding sits; it fixes the calling convention
// (docs/spec/javascript.md A3). Each row of the A3 table is one entry of
// slotTable, so R-JS-10..23 are all decided here.
type Slot string

// The slots.
const (
	// SlotDefault: property `default` (R-JS-11). this = plain object of the
	// already-set declared fields; args = [context]; document writable.
	SlotDefault Slot = "default"
	// SlotRead / SlotWrite: `@read` / `@write` (R-JS-12/13). this undefined;
	// args = [pipeline value]; document read-only. @write honours a thrown
	// HTTPResponse.
	SlotRead  Slot = "@read"
	SlotWrite Slot = "@write"
	// SlotValidate: property-level `@validate` (R-JS-14). this undefined;
	// args = [value]; honours HTTPResponse.
	SlotValidate Slot = "@validate"
	// SlotSchemaValidate: schema-level `@validate` (R-JS-15). this = the
	// serialized instance (string); args = [null].
	SlotSchemaValidate Slot = "schema-@validate"
	// SlotComputed: `@computed` (R-JS-16). this = instance view; args =
	// [context].
	SlotComputed Slot = "@computed"
	// SlotMethod: a method body called directly or across languages
	// (R-JS-17). this = receiver; args = declared parameters; Context
	// global; document only when the caller passes one (element receivers).
	SlotMethod Slot = "method"
	// SlotDispatch: a method body dispatched by composition (R-JS-18/19),
	// including doesNotUnderstand. this = the host element; document =
	// read-only host subtree; Context global.
	SlotDispatch Slot = "dispatch"
	// Trigger and processor slots (R-JS-21/22): this undefined; args =
	// [ctx]. Actions honour HTTPResponse.
	SlotTriggerWhen         Slot = "trigger-when"
	SlotTriggerAction       Slot = "trigger-action"
	SlotTriggerOtherwise    Slot = "trigger-otherwise"
	SlotProcessorWhen       Slot = "processor-when"
	SlotProcessorAction     Slot = "processor-action"
	SlotHTTPRequestProperty Slot = "httprequest-property"
	// SlotExpression: a j: expression (EvalExpression, R-JS-31).
	SlotExpression Slot = "j:"
)

type docMode int

const (
	docNone     docMode = iota // no document global, a Document is an error
	docReadOnly                // read-only stored tree when given
	docWritable                // writable (default)
)

type slotSpec struct {
	kind         string  // binding.kind for tracing
	args         int     // exact argument count; -1 = any (declared params)
	doc          docMode // ambient document
	context      bool    // Context global
	httpResponse bool    // a thrown HTTPResponse chooses the response
}

var slotTable = map[Slot]slotSpec{
	SlotDefault:             {kind: "default", args: 1, doc: docWritable},
	SlotRead:                {kind: "read", args: 1, doc: docReadOnly},
	SlotWrite:               {kind: "write", args: 1, doc: docReadOnly, httpResponse: true},
	SlotValidate:            {kind: "validate", args: 1, doc: docReadOnly, httpResponse: true},
	SlotSchemaValidate:      {kind: "schema-@validate", args: 1, doc: docReadOnly},
	SlotComputed:            {kind: "computed", args: 1, doc: docReadOnly},
	SlotMethod:              {kind: "method", args: -1, doc: docReadOnly, context: true},
	SlotDispatch:            {kind: "method", args: -1, doc: docReadOnly, context: true},
	SlotTriggerWhen:         {kind: "trigger-when", args: 1, doc: docReadOnly},
	SlotTriggerAction:       {kind: "trigger-action", args: 1, doc: docReadOnly, httpResponse: true},
	SlotTriggerOtherwise:    {kind: "trigger-otherwise", args: 1, doc: docReadOnly, httpResponse: true},
	SlotProcessorWhen:       {kind: "processor-when", args: 1, doc: docReadOnly},
	SlotProcessorAction:     {kind: "processor-action", args: 1, doc: docReadOnly, httpResponse: true},
	SlotHTTPRequestProperty: {kind: "httprequest-property", args: 1, doc: docNone},
	SlotExpression:          {kind: "j", args: -1, doc: docReadOnly, context: true},
}

func lookupSlot(s Slot) (slotSpec, error) {
	spec, ok := slotTable[s]
	if !ok {
		return slotSpec{}, fmt.Errorf("unknown slot %q", s)
	}
	return spec, nil
}
