package liquid

import "strings"

// markupContext follows the literal text of a template through a small HTML
// (or XML) tokenizer state machine, to decide for each output expression
// whether element markup may appear where it stands (R-LIQ-201): a failed
// `{{ }}` renders an inline Error item in markup context and nothing
// anywhere else (inside a tag, a comment, a raw-text element, …).
//
// Only the template's own text feeds the machine; what Liquid outputs at
// run time is unknown when the template compiles, so `<{{ tag }}>` is
// markup context and `<a href="{{ url }}">` is not.
type markupContext struct {
	xml   bool
	state ctxState
	name  strings.Builder // tag name being read
	end   bool            // reading an end tag
	raw   string          // raw-text element whose content we are in
}

type ctxState uint8

const (
	cData          ctxState = iota
	cTagName                // after `<x`, reading the name
	cTag                    // between attributes
	cBeforeValue            // after `=`
	cValueDouble            // attribute value "…"
	cValueSingle            // attribute value '…'
	cValueUnquoted          // attribute value without quotes
	cComment                // <!-- … -->
	cBogus                  // <!…>, <?…> and </… in HTML
	cRawText                // script, style, title, textarea, … (HTML)
	cCDATA                  // <![CDATA[ … ]]> (XML)
	cPI                     // <? … ?> (XML)
)

// rawTextElements are the HTML elements whose content is RAWTEXT or RCDATA.
var rawTextElements = map[string]bool{
	"script": true, "style": true, "title": true, "textarea": true,
	"xmp": true, "iframe": true, "noembed": true, "noframes": true,
}

// newMarkupContext starts the machine for a template whose host is host:
// the content of a raw-text host is itself raw text.
func newMarkupContext(host Host) *markupContext {
	m := &markupContext{xml: host.XML}
	if tag := strings.ToLower(host.Tag); !host.XML && rawTextElements[tag] {
		m.state, m.raw = cRawText, tag
	}
	return m
}

// markupAllowed reports whether an output at the current position is in
// markup context.
func (m *markupContext) markupAllowed() bool { return m.state == cData }

// decodesRefs reports whether the document's HTML parser decodes character
// references at the current position: in text, in RCDATA (title,
// textarea) and in attribute values, but not in raw text (script, style,
// …), comments, or a tag's name and attribute names. XML is left alone.
func (m *markupContext) decodesRefs() bool {
	if m.xml {
		return false
	}
	switch m.state {
	case cData, cBeforeValue, cValueDouble, cValueSingle, cValueUnquoted:
		return true
	case cRawText:
		return m.raw == "title" || m.raw == "textarea"
	}
	return false
}

func isASCIIAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func isHTMLSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r' }

// feed advances the machine over literal template text.
func (m *markupContext) feed(s string) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch m.state {
		case cData:
			if c != '<' || i+1 >= len(s) {
				continue
			}
			rest := s[i+1:]
			switch {
			case strings.HasPrefix(rest, "!--"):
				m.state = cComment
				i += 3
			case m.xml && strings.HasPrefix(rest, "![CDATA["):
				m.state = cCDATA
				i += 8
			case rest[0] == '!':
				m.state = cBogus
			case rest[0] == '?':
				m.state = cBogus
				if m.xml {
					m.state = cPI
				}
			case rest[0] == '/' && len(rest) > 1 && isASCIIAlpha(rest[1]):
				m.startTag(true)
				i++
			case rest[0] == '/' && !m.xml:
				m.state = cBogus
			case isASCIIAlpha(rest[0]) || (m.xml && (rest[0] == '_' || rest[0] >= 0x80)):
				m.startTag(false)
			}
		case cTagName:
			switch {
			case isHTMLSpace(c) || c == '/':
				m.state = cTag
			case c == '>':
				m.closeTag()
			default:
				m.name.WriteByte(c)
			}
		case cTag:
			switch c {
			case '=':
				m.state = cBeforeValue
			case '>':
				m.closeTag()
			}
		case cBeforeValue:
			switch {
			case isHTMLSpace(c):
			case c == '"':
				m.state = cValueDouble
			case c == '\'':
				m.state = cValueSingle
			case c == '>':
				m.closeTag()
			default:
				m.state = cValueUnquoted
			}
		case cValueDouble:
			if c == '"' {
				m.state = cTag
			}
		case cValueSingle:
			if c == '\'' {
				m.state = cTag
			}
		case cValueUnquoted:
			switch {
			case isHTMLSpace(c):
				m.state = cTag
			case c == '>':
				m.closeTag()
			}
		case cComment:
			if strings.HasPrefix(s[i:], "-->") {
				m.state = cData
				i += 2
			}
		case cBogus:
			if c == '>' {
				m.state = cData
			}
		case cRawText:
			if c == '<' && i+2+len(m.raw) <= len(s) && s[i+1] == '/' && strings.EqualFold(s[i+2:i+2+len(m.raw)], m.raw) {
				if j := i + 2 + len(m.raw); j == len(s) || isHTMLSpace(s[j]) || s[j] == '/' || s[j] == '>' {
					m.state, m.end, m.raw = cTag, true, ""
					i = j - 1
				}
			}
		case cCDATA:
			if strings.HasPrefix(s[i:], "]]>") {
				m.state = cData
				i += 2
			}
		case cPI:
			if strings.HasPrefix(s[i:], "?>") {
				m.state = cData
				i++
			}
		}
	}
}

func (m *markupContext) startTag(end bool) {
	m.state, m.end = cTagName, end
	m.name.Reset()
}

// closeTag handles the `>` that ends a tag: the content of an HTML raw-text
// element is raw text.
func (m *markupContext) closeTag() {
	m.state = cData
	if !m.end && !m.xml {
		if name := strings.ToLower(m.name.String()); rawTextElements[name] {
			m.state, m.raw = cRawText, name
		}
	}
	m.name.Reset()
}
