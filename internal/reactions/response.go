package reactions

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// thrown is a response chosen by a thrown HTTPResponse (R-REACT-31).
type thrown struct {
	status  int
	body    []byte
	headers [][2]string
}

// respondOutcome sends the response that ended a phase.
func (x *Reactions) respondOutcome(w http.ResponseWriter, r *http.Request, st *reqState, o outcome) {
	if o.thrown != nil {
		o.thrown.write(w, r)
		return
	}
	st.call.Fail(w, r, o.err)
}

// write sends a thrown response. It replaces the response entirely: only
// the thrown headers (plus the platform's generic ones) are sent, with
// Content-Type text/html; charset=utf-8 unless the thrown headers set one.
// Invalid fields and framing headers are dropped.
func (t *thrown) write(w http.ResponseWriter, r *http.Request) {
	status := t.status
	if status < 200 || status > 599 {
		// 0 means absent; 1xx cannot be a final response in net/http.
		status = http.StatusInternalServerError
	}
	h := w.Header()
	for _, kv := range t.headers {
		name, val := kv[0], kv[1]
		if !validHeaderName(name) || !validHeaderValue(val) {
			continue
		}
		switch strings.ToLower(name) {
		case "content-length", "transfer-encoding", "connection":
			continue
		}
		h.Add(name, val)
	}
	if h.Get("Content-Type") == "" {
		h.Set("Content-Type", "text/html; charset=utf-8")
	}
	send := bodyAllowed(status)
	if send {
		h.Set("Content-Length", strconv.Itoa(len(t.body)))
	} else {
		h.Del("Content-Length")
	}
	w.WriteHeader(status)
	if send && r.Method != http.MethodHead {
		w.Write(t.body)
	}
}

// validHeaderName reports an RFC 9110 token.
func validHeaderName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// validHeaderValue rejects control characters other than HTAB, and DEL
// (R-REACT-54).
func validHeaderValue(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 0x20 && c != '\t') || c == 0x7f {
			return false
		}
	}
	return true
}

func asErr[T error](err error, target *T) bool { return errors.As(err, target) }

// attrEsc escapes an attribute value the way PageLove documents show it
// (" as &quot;).
func attrEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// textEsc escapes text content.
func textEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
