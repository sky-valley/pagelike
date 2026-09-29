package liquid

import (
	"encoding/base64"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// String filters (R-LIQ-110 … R-LIQ-125). Every one coerces its input with
// toString; only escape, escape_once and xml_escape mark their result safe.

var (
	htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	charRef     = regexp.MustCompile(`&(?:[A-Za-z][A-Za-z0-9]*|#[0-9]+|#[xX][0-9A-Fa-f]+);`)
	lineBreak   = regexp.MustCompile(`\r?\n`)
	scriptStyle = regexp.MustCompile(`(?is)<script\b.*?</script\s*>|<style\b.*?</style\s*>|<!--.*?-->`)
	anyTag      = regexp.MustCompile(`(?s)<.*?>`)
	nonAlnum    = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

// str0 is a filter taking no argument and mapping the input string.
func str0(fn func(string) any) filterSpec {
	return filterSpec{fn: func(c *fcall) (any, error) { return fn(toString(c.in)), nil }}
}

func stringFilters() map[string]filterSpec {
	return map[string]filterSpec{
		"downcase": str0(func(s string) any { return strings.ToLower(s) }),
		"upcase":   str0(func(s string) any { return strings.ToUpper(s) }),
		"capitalize": str0(func(s string) any {
			r, n := utf8.DecodeRuneInString(s)
			if n == 0 {
				return ""
			}
			return string(unicode.ToUpper(r)) + strings.ToLower(s[n:])
		}),
		"strip":  str0(func(s string) any { return strings.TrimSpace(s) }),
		"lstrip": str0(func(s string) any { return strings.TrimLeftFunc(s, unicode.IsSpace) }),
		"rstrip": str0(func(s string) any { return strings.TrimRightFunc(s, unicode.IsSpace) }),
		"strip_newlines": str0(func(s string) any {
			return strings.NewReplacer("\r", "", "\n", "").Replace(s)
		}),
		"newline_to_br":        str0(func(s string) any { return lineBreak.ReplaceAllString(s, "<br />\n") }),
		"normalize_whitespace": str0(func(s string) any { return strings.Join(strings.Fields(s), " ") }),
		"escape":               {fn: escapeFilter},
		"xml_escape":           {fn: escapeFilter},
		"escape_once": str0(func(s string) any {
			var b strings.Builder
			for {
				loc := charRef.FindStringIndex(s)
				if loc == nil {
					b.WriteString(htmlEscaper.Replace(s))
					return SafeString(b.String())
				}
				b.WriteString(htmlEscaper.Replace(s[:loc[0]]))
				b.WriteString(s[loc[0]:loc[1]])
				s = s[loc[1]:]
			}
		}),
		"url_encode": str0(func(s string) any { return formEncode(s) }),
		"cgi_escape": str0(func(s string) any { return formEncode(s) }),
		"uri_escape": str0(func(s string) any { return encodeURI(s) }),
		"url_decode": str0(func(s string) any { return urlDecode(s) }),
		"strip_html": str0(func(s string) any {
			return anyTag.ReplaceAllString(scriptStyle.ReplaceAllString(s, ""), "")
		}),
		"replace":       replaceSpec(2, strings.ReplaceAll),
		"replace_first": replaceSpec(2, func(s, old, new string) string { return strings.Replace(s, old, new, 1) }),
		"replace_last":  replaceSpec(2, replaceLast),
		"remove":        replaceSpec(1, strings.ReplaceAll),
		"remove_first":  replaceSpec(1, func(s, old, new string) string { return strings.Replace(s, old, new, 1) }),
		"remove_last":   replaceSpec(1, replaceLast),
		"append": {min: 1, max: 1, fn: func(c *fcall) (any, error) {
			return toString(c.in) + toString(c.args[0]), nil
		}},
		"prepend": {min: 1, max: 1, fn: func(c *fcall) (any, error) {
			return toString(c.args[0]) + toString(c.in), nil
		}},
		"array_to_sentence_string": {max: 1, fn: func(c *fcall) (any, error) {
			conn := "and"
			if c.has(0) && c.args[0] != nil {
				conn = toString(c.args[0])
			}
			items, err := c.list()
			if err != nil {
				return nil, err
			}
			s := make([]string, len(items))
			for i, e := range items {
				s[i] = toString(e)
			}
			switch len(s) {
			case 0:
				return "", nil
			case 1:
				return s[0], nil
			case 2:
				return s[0] + " " + conn + " " + s[1], nil
			}
			return strings.Join(s[:len(s)-1], ", ") + ", " + conn + " " + s[len(s)-1], nil
		}},
		"slice": {min: 1, max: 2, walks: true, fn: sliceFilter},
		"split": {min: 1, max: 1, fn: func(c *fcall) (any, error) {
			s, sep := toString(c.in), toString(c.args[0])
			var parts []string
			if sep == "" {
				for _, r := range s {
					parts = append(parts, string(r))
				}
			} else {
				// Empty fields are kept, trailing ones too: "" is [""]
				// (R-LIQ-119).
				parts = strings.Split(s, sep)
			}
			out := make([]any, len(parts))
			for i, p := range parts {
				out[i] = p
			}
			return out, c.st.chargeMemory(int64(16 * len(out)))
		}},
		"truncate": {max: 2, fn: func(c *fcall) (any, error) {
			n, err := c.integer(0, 50)
			if err != nil {
				return nil, err
			}
			if n < 0 {
				return nil, filterError("length must not be negative")
			}
			suffix := c.str(1, "...")
			s := toString(c.in)
			runes := []rune(s)
			if int64(len(runes)) <= n {
				return s, nil
			}
			keep := max(n-int64(utf8.RuneCountInString(suffix)), 0)
			return string(runes[:keep]) + suffix, nil
		}},
		"truncatewords": {max: 2, fn: func(c *fcall) (any, error) {
			n, err := c.integer(0, 15)
			if err != nil {
				return nil, err
			}
			n = max(n, 1)
			suffix := c.str(1, "...")
			s := toString(c.in)
			words := strings.Fields(s)
			if int64(len(words)) <= n {
				return s, nil
			}
			return strings.Join(words[:n], " ") + suffix, nil
		}},
		"size":            {fn: func(c *fcall) (any, error) { return sizeOf(c.in), nil }},
		"number_of_words": str0(func(s string) any { return len(strings.Fields(s)) }),
		"default": {min: 1, max: 1, kwargs: []string{"allow_false"}, fn: func(c *fcall) (any, error) {
			v := c.in
			if v == false && truthy(c.kw["allow_false"]) {
				return v, nil
			}
			if v == nil || v == false || (emptyLiteral{}).matches(v) {
				return c.args[0], nil
			}
			return v, nil
		}},
		"slugify": str0(func(s string) any {
			return strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(s), "-"), "-")
		}),
		"base64_encode": str0(func(s string) any { return base64.StdEncoding.EncodeToString([]byte(s)) }),
		"base64_url_safe_encode": str0(func(s string) any {
			return base64.URLEncoding.EncodeToString([]byte(s))
		}),
		"base64_decode": {fn: func(c *fcall) (any, error) {
			b, err := base64.StdEncoding.DecodeString(toString(c.in))
			if err != nil {
				return nil, filterError("invalid base64 input")
			}
			return string(b), nil
		}},
		"base64_url_safe_decode": {fn: func(c *fcall) (any, error) {
			s := strings.TrimRight(toString(c.in), "=")
			b, err := base64.RawURLEncoding.DecodeString(s)
			if err != nil {
				return nil, filterError("invalid base64 input")
			}
			return string(b), nil
		}},
	}
}

// escapeFilter is escape and xml_escape: `& < > " '` become entities and
// the result is marked safe, so autoescaping leaves it alone. Escaping an
// already escaped value escapes it again (live 2026-09-29:
// "<" | escape | escape renders &amp;lt;; R-LIQ-91, R-LIQ-112).
func escapeFilter(c *fcall) (any, error) {
	return SafeString(htmlEscaper.Replace(toString(c.in))), nil
}

// replaceSpec builds replace (nargs 2, replacement defaults to "") and
// remove (nargs 1). An empty search string leaves the input unchanged.
func replaceSpec(nargs int, fn func(s, old, new string) string) filterSpec {
	return filterSpec{min: 1, max: nargs, fn: func(c *fcall) (any, error) {
		s, old := toString(c.in), toString(c.args[0])
		if old == "" {
			return s, nil
		}
		return fn(s, old, c.str(1, "")), nil
	}}
}

func replaceLast(s, old, new string) string {
	i := strings.LastIndex(s, old)
	if i < 0 {
		return s
	}
	return s[:i] + new + s[i+len(old):]
}

// sliceFilter is slice (R-LIQ-118): a substring by code point or a
// sub-array; a negative offset counts from the end; out of range gives ""
// or []; the length is clamped.
func sliceFilter(c *fcall) (any, error) {
	offset, err := c.integer(0, 0)
	if err != nil {
		return nil, err
	}
	length, err := c.integer(1, 1)
	if err != nil {
		return nil, err
	}
	window := func(n int) (int, int, bool) {
		from := offset
		if from < 0 {
			from += int64(n)
		}
		if from < 0 || from > int64(n) || length < 0 {
			return 0, 0, false
		}
		return int(from), int(min(from+length, int64(n))), true
	}
	if isList(c.in) {
		items, err := c.list()
		if err != nil {
			return nil, err
		}
		from, to, ok := window(len(items))
		if !ok {
			return []any{}, nil
		}
		return append([]any{}, items[from:to]...), nil
	}
	runes := []rune(toString(c.in))
	from, to, ok := window(len(runes))
	if !ok {
		return "", nil
	}
	return string(runes[from:to]), nil
}

// formEncode is url_encode and cgi_escape (R-LIQ-113): UTF-8 form encoding
// that keeps A–Z a–z 0–9 - _ . ~ * and writes a space as +.
func formEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ':
			b.WriteByte('+')
		case isASCIIAlpha(c) || isDigit(c) || strings.IndexByte("-_.~*", c) >= 0:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte("0123456789ABCDEF"[c>>4])
			b.WriteByte("0123456789ABCDEF"[c&0xF])
		}
	}
	return b.String()
}

// encodeURI is uri_escape (R-LIQ-113): JavaScript encodeURI, which keeps
// reserved characters, plus `[` and `]`.
func encodeURI(s string) string {
	const keep = "-_.!~*'();/?:@&=+$,#[]"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isASCIIAlpha(c) || isDigit(c) || strings.IndexByte(keep, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte("0123456789ABCDEF"[c>>4])
		b.WriteByte("0123456789ABCDEF"[c&0xF])
	}
	return b.String()
}

// urlDecode is url_decode: + is a space, %XX a byte; a malformed escape is
// left as written.
func urlDecode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '+':
			b.WriteByte(' ')
		case c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			b.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isHex(c byte) bool { return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') }

func unhex(c byte) byte {
	switch {
	case isDigit(c):
		return c - '0'
	case c >= 'a':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}
