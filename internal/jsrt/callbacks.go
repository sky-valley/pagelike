package jsrt

import (
	"context"
	"sync"
)

// serveCallback answers one worker callback on the host side.
func serveCallback(ctx context.Context, host Host, cb *cbMsg) *replyMsg {
	r := &replyMsg{ID: cb.ID}
	switch cb.Op {
	case "schema":
		if host == nil {
			return r
		}
		d, err := host.LookupSchema(ctx, cb.Type)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		r.Schema = d
	case "method":
		v, err := decodeValue([]byte(cb.Value), cb.Elements)
		if err != nil {
			r.Error = "bad method call: " + err.Error()
			return r
		}
		list, _ := v.([]Value)
		if len(list) == 0 {
			r.Error = "bad method call: no receiver"
			return r
		}
		if host == nil {
			r.Error = "no host to run method " + cb.Method + " of " + cb.Type
			return r
		}
		res, err := host.CallMethod(ctx, &MethodCall{Type: cb.Type, Method: cb.Method, Static: cb.Static, Receiver: list[0], Args: list[1:]})
		if err != nil {
			r.Error = err.Error()
			return r
		}
		if res == nil {
			res = &MethodResult{}
		}
		var enc valueEncoder
		if err := enc.encode(res.Value, 0); err != nil {
			r.Error = "method result: " + err.Error()
			return r
		}
		r.Value = enc.buf.String()
		for _, w := range res.ContextWrites {
			in := wireCtxIn{Name: w.Name, Deleted: w.Deleted}
			if !w.Deleted {
				start := enc.buf.Len()
				if err := enc.encode(w.Value, 0); err != nil {
					r.Error = "method context write: " + err.Error()
					return r
				}
				in.Value = enc.buf.String()[start:]
			}
			r.Context = append(r.Context, in)
		}
		r.Elements = enc.elems
	case "context":
		w := ContextWrite{Name: cb.Name, Deleted: cb.Deleted}
		if !cb.Deleted {
			v, err := decodeValue([]byte(cb.Value), cb.Elements)
			if err != nil {
				r.Error = "bad context value: " + err.Error()
				return r
			}
			w.Value = v
		}
		if host != nil {
			if err := host.WriteContext(ctx, w); err != nil {
				r.Error = err.Error()
			}
		}
	default:
		r.Error = "unknown callback " + cb.Op
	}
	return r
}

// StubHost is a Host for tests: schemas from a map, methods from functions,
// Context writes recorded.
type StubHost struct {
	Schemas map[string]*SchemaDescriptor
	// Methods maps "<type>#<method>" to an implementation.
	Methods map[string]func(*MethodCall) (*MethodResult, error)

	mu      sync.Mutex
	Writes  []ContextWrite
	Lookups []string
	Calls   []*MethodCall
}

// LookupSchema implements Host.
func (h *StubHost) LookupSchema(_ context.Context, typeURL string) (*SchemaDescriptor, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Lookups = append(h.Lookups, typeURL)
	return h.Schemas[typeURL], nil
}

// CallMethod implements Host.
func (h *StubHost) CallMethod(_ context.Context, c *MethodCall) (*MethodResult, error) {
	h.mu.Lock()
	h.Calls = append(h.Calls, c)
	f := h.Methods[c.Type+"#"+c.Method]
	h.mu.Unlock()
	if f == nil {
		return nil, &Failure{Variant: VariantThrew, Message: "TypeError: no method " + c.Method}
	}
	return f(c)
}

// WriteContext implements Host.
func (h *StubHost) WriteContext(_ context.Context, w ContextWrite) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Writes = append(h.Writes, w)
	return nil
}
