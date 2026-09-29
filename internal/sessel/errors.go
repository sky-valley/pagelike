package sessel

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sky-valley/pagelike/internal/dom"
)

// ParseError reports a program that does not match the grammar. It is
// raised before evaluation and is never caught by try/catch (R-SESSEL-40).
type ParseError struct {
	Msg    string
	Offset int // byte offset in the program
	Line   int // 1-based
	Col    int // 1-based, in bytes
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("parse error at %d:%d: %s", e.Line, e.Col, e.Msg)
}

func newParseError(src string, off int, msg string) *ParseError {
	if off > len(src) {
		off = len(src)
	}
	line := 1 + strings.Count(src[:off], "\n")
	col := off + 1
	if i := strings.LastIndexByte(src[:off], '\n'); i >= 0 {
		col = off - i
	}
	return &ParseError{Msg: msg, Offset: off, Line: line, Col: col}
}

// Error categories (R-SESSEL-340).
const (
	TypeErrorType    = "TypeError"
	RuntimeErrorType = "RuntimeError"
	UserErrorType    = "Error"
)

// Reason refines a runtime error for hosts that map failures to statuses.
type Reason int

// Reasons.
const (
	ReasonNone        Reason = iota
	ReasonUnresolved         // an unresolved variable
	ReasonSelfUnbound        // self referenced where it is unbound (QUERY on a directory → 416)
	ReasonBudget             // ops or memory budget exhausted
	ReasonTimeout            // wall-clock budget exhausted or context cancelled
	ReasonDepth              // evaluation depth limit
	ReasonNoWriter           // Pagelove.PUT/DELETE without a write provider
	ReasonThrow              // a user throw
)

// Error is a runtime error: catchable by try/catch, with a category (Type)
// and a message. A user `throw` produces an Error with Thrown set and the
// thrown value in Value.
type Error struct {
	Type    string
	Message string
	Reason  Reason
	Thrown  bool
	Value   Value // the thrown value (user throws)
}

func (e *Error) Error() string { return e.Type + ": " + e.Message }

func typeErr(format string, args ...any) *Error {
	return &Error{Type: TypeErrorType, Message: fmt.Sprintf(format, args...)}
}

func runtimeErr(format string, args ...any) *Error {
	return &Error{Type: RuntimeErrorType, Message: fmt.Sprintf(format, args...)}
}

// AsError returns the runtime error in err, if any.
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// IsParseError reports whether err is a parse error.
func IsParseError(err error) bool {
	var pe *ParseError
	return errors.As(err, &pe)
}

// errorValue is what a catch variable binds for err (R-SESSEL-341/342).
func errorValue(e *Error) Value {
	if e.Thrown {
		switch v := e.Value.(type) {
		case *Dict:
			m, _ := v.Get("message")
			t, _ := v.Get("type")
			if _, ok := m.(string); ok {
				if _, ok := t.(string); ok {
					return v
				}
			}
		case *Element:
			if v.Class != nil {
				return v
			}
		}
		d := NewDict()
		d.Set("message", e.Message)
		d.Set("type", e.Type)
		d.Set("value", e.Value)
		return d
	}
	d := NewDict()
	d.Set("message", e.Message)
	d.Set("type", e.Type)
	return d
}

// throwError builds the error raised by `throw v` (R-SESSEL-342).
func throwError(v Value) *Error {
	e := &Error{Type: UserErrorType, Thrown: true, Value: v, Reason: ReasonThrow}
	switch x := v.(type) {
	case *Dict:
		m, _ := x.Get("message")
		t, _ := x.Get("type")
		ms, ok1 := m.(string)
		ts, ok2 := t.(string)
		if ok1 && ok2 {
			e.Type, e.Message = ts, ms
			return e
		}
	case *Element:
		if x.Class != nil {
			e.Message = "thrown " + x.Class.URL()
			if r, ok := responseOfElement(x); ok {
				e.Message = fmt.Sprintf("HTTPResponse %d", r.Status)
				if r.Message != "" {
					e.Message += ": " + r.Message
				}
			}
			return e
		}
	}
	e.Message = TextOf(v)
	return e
}

// HTTPResponse is a response raised by `throw new HTTPResponse { … }`
// (R-SESSEL-294). Hosts check for it with ResponseOf.
type HTTPResponse struct {
	Status     int
	Message    string
	HasMessage bool
	Body       string
	HasBody    bool
	Headers    [][2]string // in declaration order
}

// Content returns the body to send: body if present, else message.
func (r *HTTPResponse) Content() string {
	if r.HasBody {
		return r.Body
	}
	return r.Message
}

// HTMLBody is Content parsed as HTML and serialized again, as PageLove does
// for Sessel-thrown responses (R-SESSEL-247, R-REACT-32): a string starting
// with <!DOCTYPE or <html is a document, anything else a <body> fragment,
// so markup round-trips and other text comes back escaped (=> as =&gt;).
func (r *HTTPResponse) HTMLBody() string {
	s := r.Content()
	t := strings.ToLower(strings.TrimLeft(s, " \t\r\n\f"))
	if strings.HasPrefix(t, "<!doctype") || strings.HasPrefix(t, "<html") {
		if doc, err := dom.Parse([]byte(s)); err == nil {
			return string(dom.Render(doc))
		}
		return s
	}
	nodes, err := dom.ParseBodyFragment(s)
	if err != nil {
		return s
	}
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(dom.OuterHTML(n))
	}
	return b.String()
}

// ResponseOf extracts an HTTPResponse from an error raised by a Sessel
// throw of an HTTPResponse instance.
func ResponseOf(err error) (*HTTPResponse, bool) {
	e, ok := AsError(err)
	if !ok || !e.Thrown {
		return nil, false
	}
	el, ok := e.Value.(*Element)
	if !ok {
		return nil, false
	}
	return responseOfElement(el)
}

func responseOfElement(el *Element) (*HTTPResponse, bool) {
	if el.Class == nil || el.Class.URL() != URLHTTPResponse {
		return nil, false
	}
	r := &HTTPResponse{Status: 500}
	for _, p := range itemProps(el.Node) {
		switch p.name {
		case "status":
			if n, err := strconv.Atoi(strings.TrimSpace(elementValueString(p.node))); err == nil {
				r.Status = n
			}
		case "message":
			if !r.HasMessage {
				r.Message, r.HasMessage = elementValueString(p.node), true
			}
		case "body":
			if !r.HasBody {
				r.Body, r.HasBody = elementValueString(p.node), true
			}
		case "header":
			var k, v string
			for _, hp := range itemProps(p.node) {
				switch hp.name {
				case "key":
					k = elementValueString(hp.node)
				case "value":
					v = elementValueString(hp.node)
				}
			}
			if k != "" {
				r.Headers = append(r.Headers, [2]string{k, v})
			}
		}
	}
	if r.Status < 100 || r.Status > 599 {
		r.Status = 500
	}
	return r, true
}
