package httpapi

import (
	"net/http"

	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/site"
)

// Interceptor wraps the core processing of public-plane requests. The
// reactions package installs one (Public.Intercept): triggers run before
// core processing and processors over its response (docs/spec/reacting.md
// R-REACT-8). It is not called for the built-in identity endpoints, the
// /-pagelike/ API, or requests whose path fails normalization.
//
// An interceptor that has nothing to do must call c.Next(w, r) unchanged,
// so the response is exactly what core processing produces.
type Interceptor interface {
	Intercept(w http.ResponseWriter, r *http.Request, c *Call)
}

// Call describes one intercepted request and gives the interceptor the
// public plane's core processing and error rendering.
type Call struct {
	Site      *site.Site
	Principal *identity.Principal
	// Host is the request host without port.
	Host string
	// Scheme is "https" when the request arrived over TLS (or through a
	// trusted proxy that says so), else "http".
	Scheme string
	// Path is the normalized request path (a directory keeps its slash).
	Path string
	// Subscribe reports an SSE subscription (GET with an explicit
	// Accept: text/event-stream), whose response is a stream.
	Subscribe bool
	// Next runs core processing for r (whose body the interceptor may have
	// replaced) and writes its response to w.
	Next func(w http.ResponseWriter, r *http.Request)
	// Fail writes err as the public plane's error document.
	Fail func(w http.ResponseWriter, r *http.Request, err error)
	// ReadBody reads r's body under the site's size cap (413 when it is
	// exceeded, which also closes the connection).
	ReadBody func(w http.ResponseWriter, r *http.Request) ([]byte, error)
}

// call builds the Call for a request whose core processing is serveMethod.
func (p *Public) call(r *http.Request, rc *reqCtx, clean string) *Call {
	scheme := "http"
	if identity.IsTLS(r, p.TrustProxy) {
		scheme = "https"
	}
	return &Call{
		Site: rc.site, Principal: rc.principal, Host: rc.host, Scheme: scheme, Path: clean,
		Subscribe: r.Method == http.MethodGet && engine.AcceptQuality(r.Header.Get("Accept"), "text/event-stream", true) > 0,
		Next:      func(w http.ResponseWriter, r *http.Request) { p.serveMethod(w, r, rc, clean) },
		Fail:      func(w http.ResponseWriter, r *http.Request, err error) { p.fail(w, r, rc, err) },
		ReadBody:  func(w http.ResponseWriter, r *http.Request) ([]byte, error) { return p.readBody(w, r, rc) },
	}
}
