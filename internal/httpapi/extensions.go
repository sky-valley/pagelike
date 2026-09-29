package httpapi

import (
	"net/http"

	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/site"
)

// ExtRequest is what a /-pagelike/ extension endpoint sees of a request.
type ExtRequest struct {
	Site      *site.Site
	Principal *identity.Principal
	Host      string
	Public    *Public
}

// Extension serves a pagelike-specific endpoint under /-pagelike/. It
// reports whether it handled the request. Extensions never shadow PageLove
// paths: they are only consulted for /-pagelike/ URLs.
type Extension func(w http.ResponseWriter, r *http.Request, x *ExtRequest) bool

var extensions []Extension

// RegisterExtension adds a /-pagelike/ endpoint handler (call from init).
func RegisterExtension(f Extension) { extensions = append(extensions, f) }

func (p *Public) runExtensions(w http.ResponseWriter, r *http.Request, rc *reqCtx) bool {
	x := &ExtRequest{Site: rc.site, Principal: rc.principal, Host: rc.host, Public: p}
	for _, f := range extensions {
		if f(w, r, x) {
			return true
		}
	}
	return false
}

// Fail renders err as a PageLove-style error document (for extensions).
func (p *Public) Fail(w http.ResponseWriter, r *http.Request, x *ExtRequest, err error) {
	p.fail(w, r, &reqCtx{site: x.Site, principal: x.Principal, host: x.Host}, err)
}
