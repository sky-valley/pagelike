package compose

import (
	"strings"
)

// Start-tag rewriting. Composition changes only the directive attributes of
// an element it keeps (R-LIQ-34): the rest of the start tag — attribute
// order, quoting, character references, whitespace — is copied from the
// source. The scanner follows the HTML tokenizer's attribute rules, which
// also cover the XML start tags pagelike's lenient XML parser accepts.

// tagAttr is one attribute of a raw start tag: text[start:end] is the
// attribute (name, optional "=" and value), text[ws:start] the whitespace
// before it.
type tagAttr struct {
	name             string // as written
	ws, start, end   int
	valStart, valEnd int // value text (without quotes); -1 when valueless
}

func isTagSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}

// scanStartTag lists the attributes of a raw start tag ("<name …>").
func scanStartTag(tag string) []tagAttr {
	i := 1
	for i < len(tag) && !isTagSpace(tag[i]) && tag[i] != '/' && tag[i] != '>' {
		i++
	}
	var out []tagAttr
	for i < len(tag) {
		ws := i
		for i < len(tag) && (isTagSpace(tag[i]) || tag[i] == '/') {
			if tag[i] == '/' && i+1 < len(tag) && tag[i+1] == '>' {
				return out
			}
			i++
		}
		if i >= len(tag) || tag[i] == '>' {
			return out
		}
		start := i
		i++ // the first character belongs to the name even when it is '='
		for i < len(tag) && !isTagSpace(tag[i]) && tag[i] != '/' && tag[i] != '>' && tag[i] != '=' {
			i++
		}
		a := tagAttr{name: tag[start:i], ws: ws, start: start, valStart: -1, valEnd: -1}
		j := i
		for j < len(tag) && isTagSpace(tag[j]) {
			j++
		}
		if j < len(tag) && tag[j] == '=' {
			j++
			for j < len(tag) && isTagSpace(tag[j]) {
				j++
			}
			if j < len(tag) && (tag[j] == '"' || tag[j] == '\'') {
				q := tag[j]
				k := strings.IndexByte(tag[j+1:], q)
				if k < 0 {
					a.valStart, a.valEnd, j = j+1, len(tag), len(tag)
				} else {
					a.valStart, a.valEnd, j = j+1, j+1+k, j+2+k
				}
			} else {
				vs := j
				for j < len(tag) && !isTagSpace(tag[j]) && tag[j] != '>' {
					j++
				}
				a.valStart, a.valEnd = vs, j
			}
			i = j
		}
		a.end = i
		out = append(out, a)
	}
	return out
}

// tagEdit describes how to rewrite one start tag: attributes to drop (by
// name, compared as the parser stores names) and attributes to set.
type tagEdit struct {
	drop map[string]bool
	set  [][2]string // name, value (added when absent)
}

func (e *tagEdit) empty() bool { return e == nil || (len(e.drop) == 0 && len(e.set) == 0) }

func (e *tagEdit) dropAttr(name string) {
	if e.drop == nil {
		e.drop = map[string]bool{}
	}
	e.drop[name] = true
}

// rewriteStartTag applies e to the raw start tag. xml selects
// case-sensitive attribute names (HTML names compare ASCII-lower-cased, as
// the HTML parser stores them).
func rewriteStartTag(tag string, e *tagEdit, xml bool) string {
	if e.empty() {
		return tag
	}
	norm := func(s string) string {
		if xml {
			return s
		}
		return strings.ToLower(s)
	}
	attrs := scanStartTag(tag)
	var b strings.Builder
	pos := 0
	done := map[string]bool{}
	for _, a := range attrs {
		n := norm(a.name)
		if e.drop[n] {
			b.WriteString(tag[pos:a.ws])
			pos = a.end
			continue
		}
		for _, kv := range e.set {
			if norm(kv[0]) == n && !done[n] {
				done[n] = true
				b.WriteString(tag[pos:a.start])
				b.WriteString(a.name + `="` + escapeAttr(kv[1]) + `"`)
				pos = a.end
			}
		}
	}
	// New attributes go after the last attribute (before "/>" or ">").
	insertAt := tagNameEnd(tag)
	if len(attrs) > 0 {
		insertAt = attrs[len(attrs)-1].end
	}
	if insertAt < pos {
		insertAt = pos
	}
	b.WriteString(tag[pos:insertAt])
	for _, kv := range e.set {
		if !done[norm(kv[0])] {
			b.WriteString(" " + kv[0] + `="` + escapeAttr(kv[1]) + `"`)
		}
	}
	b.WriteString(tag[insertAt:])
	return b.String()
}

func tagNameEnd(tag string) int {
	i := 1
	for i < len(tag) && !isTagSpace(tag[i]) && tag[i] != '/' && tag[i] != '>' {
		i++
	}
	return i
}

var attrEscaper = strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;")

func escapeAttr(s string) string { return attrEscaper.Replace(s) }

var textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// escapeText escapes a scalar inserted as text (R-COMP-62).
func escapeText(s string) string { return textEscaper.Replace(s) }
