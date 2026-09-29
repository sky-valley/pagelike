package liquid

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/osteele/liquid/values"
)

// JSON for templates (R-LIQ-190): compact by default, pretty with an
// indent; strings are not HTML-escaped; integral floats keep `.0`; *Hash and
// items keep their order and plain maps sort their keys.

const maxJSONDepth = 512

var errJSONDepth = errors.New("json: value nested too deeply")

// encodeJSON serializes v; indent > 0 pretty-prints with that many spaces.
func encodeJSON(v any, indent int) (string, error) {
	var b bytes.Buffer
	if err := writeJSON(&b, v, 0); err != nil {
		return "", err
	}
	if indent <= 0 {
		return b.String(), nil
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b.Bytes(), "", strings.Repeat(" ", indent)); err != nil {
		return "", err
	}
	return out.String(), nil
}

func writeJSON(b *bytes.Buffer, v any, depth int) error {
	if depth > maxJSONDepth {
		return errJSONDepth
	}
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
		return nil
	case bool:
		b.WriteString(strconv.FormatBool(x))
		return nil
	case string:
		writeJSONString(b, x)
		return nil
	case SafeString:
		writeJSONString(b, string(x))
		return nil
	case emptyLiteral:
		b.WriteString(`""`)
		return nil
	case float64:
		writeJSONFloat(b, x)
		return nil
	case float32:
		writeJSONFloat(b, float64(x))
		return nil
	case []byte:
		writeJSONString(b, string(x))
		return nil
	case time.Time:
		writeJSONString(b, x.UTC().Format(time.RFC3339))
		return nil
	case *Item:
		return writeItemJSON(b, x, depth)
	case *Request:
		return writeJSON(b, x.whole(), depth)
	case *Hash:
		b.WriteByte('{')
		for i, k := range x.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, k)
			b.WriteByte(':')
			if err := writeJSON(b, x.m[k], depth+1); err != nil {
				return err
			}
		}
		b.WriteByte('}')
		return nil
	case values.Range:
		return writeJSON(b, toArray(x), depth)
	}
	if i, _, isInt, ok := goNumber(v); ok && isInt {
		b.WriteString(strconv.FormatInt(i, 10))
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		b.WriteByte('[')
		for i := range rv.Len() {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeJSON(b, rv.Index(i).Interface(), depth+1); err != nil {
				return err
			}
		}
		b.WriteByte(']')
		return nil
	case reflect.Map:
		keys := make([]string, 0, rv.Len())
		byKey := map[string]reflect.Value{}
		for _, k := range rv.MapKeys() {
			ks := fmt.Sprint(k.Interface())
			keys = append(keys, ks)
			byKey[ks] = rv.MapIndex(k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, k)
			b.WriteByte(':')
			if err := writeJSON(b, byKey[k].Interface(), depth+1); err != nil {
				return err
			}
		}
		b.WriteByte('}')
		return nil
	}
	writeJSONString(b, toString(v))
	return nil
}

// writeItemJSON writes an item as {"@id", "@type", properties in
// first-occurrence document order}.
func writeItemJSON(b *bytes.Buffer, it *Item, depth int) error {
	it.load()
	b.WriteString(`{"@id":`)
	id, _ := it.get("@id")
	writeJSONString(b, id.(string))
	if t, ok := it.get("@type"); ok {
		b.WriteString(`,"@type":`)
		writeJSONString(b, t.(string))
	}
	for _, k := range it.names {
		b.WriteByte(',')
		writeJSONString(b, k)
		b.WriteByte(':')
		if err := writeJSON(b, it.props[k], depth+1); err != nil {
			return err
		}
	}
	b.WriteByte('}')
	return nil
}

// writeJSONFloat writes integral floats with ".0" (R-LIQ-190) and
// non-finite ones as null, which JSON cannot represent otherwise.
func writeJSONFloat(b *bytes.Buffer, f float64) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		b.WriteString("null")
		return
	}
	b.WriteString(formatFloat(f))
}

const hexDigits = "0123456789abcdef"

// writeJSONString quotes s like encoding/json with HTML escaping off.
func writeJSONString(b *bytes.Buffer, s string) {
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
				b.WriteByte(hexDigits[c>>4])
				b.WriteByte(hexDigits[c&0xF])
			default:
				b.WriteByte(c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			b.WriteString(`�`)
		case r == ' ' || r == ' ':
			b.WriteString(`\u202`)
			b.WriteByte(hexDigits[r&0xF])
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	b.WriteByte('"')
}

// DecodeJSON decodes JSON into template values: objects become *Hash in
// document order, integral numbers within ±2^53 become int (R-LIQ-59: the
// JavaScript binding renders `60`, not `60.0`), other numbers float64.
// Composition can use it for `j:` binding results.
func DecodeJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeJSONValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err == nil {
		return nil, errors.New("json: trailing data")
	}
	return v, nil
}

func decodeJSONValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			h := NewHash()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				h.Set(k, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return h, nil
		case '[':
			a := []any{}
			for dec.More() {
				v, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return a, nil
		}
		return nil, fmt.Errorf("json: unexpected %v", t)
	case json.Number:
		if i, err := t.Int64(); err == nil && i >= -(1<<53) && i <= 1<<53 {
			return mkInt(i), nil
		}
		f, err := t.Float64()
		if err != nil {
			return nil, err
		}
		if f == math.Trunc(f) && math.Abs(f) <= 1<<53 {
			return mkInt(int64(f)), nil
		}
		return f, nil
	}
	return tok, nil
}
