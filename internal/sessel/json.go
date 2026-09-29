package sessel

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"
)

// MediaType is the media type of EncodeJSON output.
const MediaType = "application/sessel+json"

// EncodeJSON encodes v as application/sessel+json (R-SESSEL-97): Integers as
// JSON integers, Floats with a decimal point kept for integral values
// (100.0), dictionaries in insertion order, elements (and Instances) as
// {"$type":"element","$html":…,"$source":…} with $source omitted for
// constructed elements, Temporal values as ISO strings and Selectors as
// their source. Lambdas, Classes and Blobs are a TypeError.
func EncodeJSON(v Value) ([]byte, error) {
	var b bytes.Buffer
	if err := encodeJSON(&b, v, 0, false); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// EncodeQueryJSON encodes a QUERY result as live PageLove answers it: like
// EncodeJSON, but element objects carry no $source (live 2026-09-28,
// decisions.md protocol.query-sessel.element-list-tagged).
func EncodeQueryJSON(v Value) ([]byte, error) {
	var b bytes.Buffer
	if err := encodeJSON(&b, v, 0, true); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func encodeJSON(b *bytes.Buffer, v Value, depth int, noSource bool) error {
	if depth > 512 {
		return runtimeErr("value nests too deeply to encode")
	}
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return typeErr("cannot encode a non-finite number")
		}
		b.WriteString(FormatFloat(x))
	case string:
		writeJSONString(b, x)
	case List:
		b.WriteByte('[')
		for i, it := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := encodeJSON(b, it, depth+1, noSource); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case *Dict:
		b.WriteByte('{')
		for i, k := range x.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, k)
			b.WriteByte(':')
			if err := encodeJSON(b, x.vals[k], depth+1, noSource); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case *Element:
		b.WriteString(`{"$type":"element","$html":`)
		writeJSONString(b, outerHTML(x.Node))
		if x.Doc != nil && x.Doc.Path != "" && !noSource {
			b.WriteString(`,"$source":`)
			writeJSONString(b, x.Doc.Path)
		}
		b.WriteByte('}')
	case temporal:
		writeJSONString(b, x.String())
	case *SelectorValue:
		writeJSONString(b, x.Source)
	default:
		return typeErr("cannot encode %s as application/sessel+json", TypeName(v))
	}
	return nil
}

func writeJSONString(b *bytes.Buffer, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch {
			case c == '"' || c == '\\':
				b.WriteByte('\\')
				b.WriteByte(c)
			case c == '\n':
				b.WriteString(`\n`)
			case c == '\r':
				b.WriteString(`\r`)
			case c == '\t':
				b.WriteString(`\t`)
			case c < 0x20:
				b.WriteString(`\u00`)
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0xf])
			default:
				b.WriteByte(c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteString(`�`)
		} else if r == ' ' || r == ' ' {
			b.WriteString(`\u202`)
			b.WriteByte(hex[r&0xf])
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	b.WriteByte('"')
}

// FromGo converts a Go value to a Sessel value: nil, bool, all integer and
// float kinds, string, []any, []string, map[string]any (keys sorted),
// time.Time (Instant), json.Number, and Sessel values unchanged.
func FromGo(v any) Value {
	switch x := v.(type) {
	case nil:
		return nil
	case bool, string, int64, float64, List, *Dict, *Element, *SelectorValue, *Lambda, *TypeNS, *Blob, temporal:
		return x
	case int:
		return int64(x)
	case int8:
		return int64(x)
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case uint:
		return int64(x)
	case uint8:
		return int64(x)
	case uint16:
		return int64(x)
	case uint32:
		return int64(x)
	case uint64:
		if x > math.MaxInt64 {
			return float64(x)
		}
		return int64(x)
	case float32:
		return float64(x)
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}
		f, _ := x.Float64()
		return f
	case []any:
		out := make(List, len(x))
		for i, it := range x {
			out[i] = FromGo(it)
		}
		return out
	case []string:
		out := make(List, len(x))
		for i, it := range x {
			out[i] = it
		}
		return out
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		d := NewDict()
		for _, k := range keys {
			d.Set(k, FromGo(x[k]))
		}
		return d
	case map[string]string:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		d := NewDict()
		for _, k := range keys {
			d.Set(k, x[k])
		}
		return d
	case time.Time:
		return Instant{NS: x.UnixNano()}
	case Class:
		return x
	}
	return nil
}

// ToGo converts a Sessel value to plain Go data (the JSON-like model shared
// with server JavaScript): nil, bool, int64, float64, string, []any,
// map[string]any, and tagged maps for elements
// ({"$type":"element","$html":…,"$source":…}). Temporal values and
// Selectors become strings; other values nil.
func ToGo(v Value) any {
	switch x := v.(type) {
	case nil, bool, int64, float64, string:
		return x
	case List:
		out := make([]any, len(x))
		for i, it := range x {
			out[i] = ToGo(it)
		}
		return out
	case *Dict:
		out := make(map[string]any, x.Len())
		for _, k := range x.keys {
			out[k] = ToGo(x.vals[k])
		}
		return out
	case *Element:
		m := map[string]any{"$type": "element", "$html": outerHTML(x.Node)}
		if x.Doc != nil && x.Doc.Path != "" {
			m["$source"] = x.Doc.Path
		}
		return m
	case temporal:
		return x.String()
	case *SelectorValue:
		return x.Source
	}
	return nil
}
