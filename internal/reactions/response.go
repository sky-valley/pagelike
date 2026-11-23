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
		if cw, ok := w.(*cacheWriter); ok {
			cw.thrown = true
		}
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

// cacheWriter keeps a GET or HEAD that a trigger or processor shaped out of
// shared caches (docs GET-method §Caching, snapshot 2026-11-23; R-RW-155):
// such a response is sent with Cache-Control: private, no-store, a 304
// included, and a private max-age is rewritten. Only a thrown HTTPResponse
// can opt back in, with its own public, max-age or s-maxage directive. A
// response that sets a cookie never opts in.
type cacheWriter struct {
	http.ResponseWriter
	st     *reqState
	thrown bool // the response is a thrown HTTPResponse
	done   bool
}

func (c *cacheWriter) WriteHeader(code int) {
	if !c.done {
		c.done = true
		if c.st.shaped.Load() {
			h := c.Header()
			cc := strings.ToLower(h.Get("Cache-Control"))
			if !c.thrown || h.Get("Set-Cookie") != "" || !sharedCacheable(cc) {
				h.Set("Cache-Control", "private, no-store")
			}
		}
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *cacheWriter) Write(b []byte) (int, error) {
	if !c.done {
		c.WriteHeader(http.StatusOK)
	}
	return c.ResponseWriter.Write(b)
}

func (c *cacheWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *cacheWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

// sharedCacheable reports whether a Cache-Control value lets shared caches
// store the response: public, s-maxage, or a max-age that is not private.
func sharedCacheable(cc string) bool {
	if strings.Contains(cc, "no-store") || strings.Contains(cc, "private") {
		return false
	}
	return strings.Contains(cc, "public") || strings.Contains(cc, "s-maxage") || strings.Contains(cc, "max-age")
}
