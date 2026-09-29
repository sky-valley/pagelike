package engine

import (
	"errors"
	"io"
	"net/http"

	"github.com/sky-valley/pagelike/internal/errdoc"
)

// ReadBody reads a request body under a site's cap (docs/spec/
// reading-writing.md R-RW-120..122), for both planes: a declared
// Content-Length over the cap is refused before anything is read, a
// streamed body as soon as it exceeds the cap (413, closing the
// connection); a body shorter than declared is 400. A body exactly at the
// cap is accepted.
func ReadBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	tooLarge := func() error {
		w.Header().Set("Connection", "close")
		return errdoc.New(http.StatusRequestEntityTooLarge, "ContentTooLarge", "request body exceeds the %d-byte limit", limit)
	}
	if r.ContentLength > limit {
		return nil, tooLarge()
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, tooLarge()
		}
		return nil, errdoc.New(http.StatusBadRequest, "BadBody", "cannot read the request body: %v", err)
	}
	return body, nil
}

// ConventionalHeaders wraps w so that header names Go's canonicalization
// spells unconventionally (Etag, Www-Authenticate) go out as HTTP
// convention and PageLove's documentation spell them (docs/spec/protocol.md
// R-PROTO-1). Clients compare names case-insensitively; this is cosmetic.
func ConventionalHeaders(w http.ResponseWriter) http.ResponseWriter {
	return &casedWriter{ResponseWriter: w}
}

type casedWriter struct {
	http.ResponseWriter
	wrote bool
}

var conventional = map[string]string{"Etag": "ETag", "Www-Authenticate": "WWW-Authenticate"}

func (c *casedWriter) WriteHeader(code int) {
	if !c.wrote {
		c.wrote = true
		h := c.Header()
		for canon, conv := range conventional {
			if v, ok := h[canon]; ok {
				delete(h, canon)
				h[conv] = v
			}
		}
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *casedWriter) Write(b []byte) (int, error) {
	if !c.wrote {
		c.WriteHeader(http.StatusOK)
	}
	return c.ResponseWriter.Write(b)
}

// Flush supports streaming responses (Server-Sent Events).
func (c *casedWriter) Flush() {
	if !c.wrote {
		c.WriteHeader(http.StatusOK)
	}
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (c *casedWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }
