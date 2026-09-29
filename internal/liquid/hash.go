package liquid

import (
	"sort"

	"github.com/osteele/liquid/values"
)

// Hash is an insertion-ordered hash with string keys (R-LIQ-50). JSON keeps
// its order (R-LIQ-190). Composition should pass Sessel dictionaries and
// JavaScript objects as *Hash; a plain map[string]any is accepted too, and
// serializes with its keys sorted.
//
// A Hash is not safe for concurrent mutation; build it before rendering.
type Hash struct {
	keys []string
	m    map[string]any
}

// NewHash returns an empty hash.
func NewHash() *Hash { return &Hash{m: map[string]any{}} }

// HashOf copies a map into a hash, with its keys sorted.
func HashOf(m map[string]any) *Hash {
	h := &Hash{keys: make([]string, 0, len(m)), m: make(map[string]any, len(m))}
	for k := range m {
		h.keys = append(h.keys, k)
	}
	sort.Strings(h.keys)
	for _, k := range h.keys {
		h.m[k] = m[k]
	}
	return h
}

// Set adds or replaces a member; a new key goes last. It returns h.
func (h *Hash) Set(key string, v any) *Hash {
	if h.m == nil {
		h.m = map[string]any{}
	}
	if _, ok := h.m[key]; !ok {
		h.keys = append(h.keys, key)
	}
	h.m[key] = v
	return h
}

// Get returns a member.
func (h *Hash) Get(key string) (any, bool) {
	if h == nil {
		return nil, false
	}
	v, ok := h.m[key]
	return v, ok
}

// Keys returns the keys in insertion order.
func (h *Hash) Keys() []string {
	if h == nil {
		return nil
	}
	return append([]string(nil), h.keys...)
}

// Len returns the number of members.
func (h *Hash) Len() int {
	if h == nil {
		return 0
	}
	return len(h.keys)
}

// Interface implements values.Value.
func (h *Hash) Interface() any { return h }

// Int implements values.Value.
func (h *Hash) Int() int { panic(values.TypeError("can't convert hash to int")) }

// Equal implements values.Value.
func (h *Hash) Equal(o values.Value) bool { return strictEqual(h, o.Interface()) }

// Less implements values.Value: hashes are not ordered.
func (h *Hash) Less(values.Value) bool { return false }

// Contains implements values.Value. `contains` is a substring or membership
// test, false for hashes (R-LIQ-74).
func (h *Hash) Contains(values.Value) bool { return false }

// IndexValue implements values.Value: `h['key']`.
func (h *Hash) IndexValue(k values.Value) values.Value {
	if key, ok := k.Interface().(string); ok {
		if v, found := h.Get(key); found {
			return values.ValueOf(v)
		}
	}
	return undefinedValue
}

// PropertyValue implements values.Value: `h.key`, and `h.size` as the key
// count when there is no `size` key (R-LIQ-55).
func (h *Hash) PropertyValue(k values.Value) values.Value {
	key, ok := k.Interface().(string)
	if !ok {
		return undefinedValue
	}
	if v, found := h.Get(key); found {
		return values.ValueOf(v)
	}
	if key == "size" {
		return values.ValueOf(h.Len())
	}
	return undefinedValue
}

// Test implements values.Value: every hash is truthy.
func (h *Hash) Test() bool { return true }
