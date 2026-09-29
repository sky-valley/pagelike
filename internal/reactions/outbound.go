package reactions

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// outRequest is a fully evaluated outbound request (R-REACT-48..56), as
// recorded in the outbox.
type outRequest struct {
	Kind     string      `json:"kind"` // kindHTTP or kindTransition
	URL      string      `json:"url"`
	Method   string      `json:"method"`
	Header   [][2]string `json:"header"`
	Body     []byte      `json:"body,omitempty"`
	Attempts int         `json:"attempts"` // 1 + min(retry, 4); 1 for transitions
}

const (
	kindHTTP       = "http"       // generic action: bounded retries (R-REACT-57)
	kindTransition = "transition" // handler delivery: at most once (R-REACT-82)
)

var outboundMethods = map[string]bool{"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true}

// outbound evaluates an HttpRequest action of a trigger or processor into a
// queued request (R-REACT-49..55). A nil request means the action is
// dropped (no url, an unsupported scheme or method). lang names the
// language of a failing dynamic property.
func (e *evalEnv) outbound(ctx context.Context, a *httpAction) (*outRequest, string, error) {
	eval := func(v *value) (string, bool, string, error) {
		s, ok, err := e.value(ctx, v)
		lang := langSessel
		if v != nil && v.lang == langJS {
			lang = langJS
		}
		return s, ok, lang, err
	}
	raw, ok, lang, err := eval(a.url)
	if err != nil {
		return nil, lang, err
	}
	if !ok {
		return nil, "", nil
	}
	target, ok := resolveURL(raw, e.st.origin)
	if !ok {
		return nil, "", nil
	}
	method := "POST"
	if a.method != nil {
		m, ok, lang, err := eval(a.method)
		if err != nil {
			return nil, lang, err
		}
		if ok {
			method = strings.ToUpper(strings.TrimSpace(m))
		}
	}
	if !outboundMethods[method] {
		return nil, "", nil
	}
	ctype := "text/html"
	if a.contentType != nil {
		c, ok, lang, err := eval(a.contentType)
		if err != nil {
			return nil, lang, err
		}
		if ok {
			ctype = c
		}
	}
	var body string
	if a.body != nil {
		b, _, lang, err := eval(a.body)
		if err != nil {
			return nil, lang, err
		}
		body = b
	}
	if len(body) > MaxOutboundBody {
		return nil, langSessel, fmt.Errorf("outbound request body exceeds %d bytes", MaxOutboundBody)
	}
	var headers [][2]string
	for _, h := range a.headers {
		if h.key == "" {
			continue
		}
		v, _, lang, err := eval(h.value)
		if err != nil {
			return nil, lang, err
		}
		if !validHeaderName(h.key) || !validHeaderValue(v) {
			continue // R-REACT-54
		}
		switch strings.ToLower(h.key) {
		case "host", "content-length", "transfer-encoding", "connection":
			continue
		case "content-type":
			ctype = v // Content-Type has one home (R-REACT-53)
			continue
		}
		headers = append(headers, [2]string{h.key, v})
	}
	if validHeaderValue(ctype) {
		headers = append([][2]string{{"Content-Type", ctype}}, headers...)
	}
	return &outRequest{Kind: kindHTTP, URL: target, Method: method, Header: headers, Body: []byte(body), Attempts: 1 + a.retry}, "", nil
}

// resolveURL applies R-REACT-49: an absolute http(s) URL is used as given;
// a path is resolved against the site's own public origin; anything else
// drops the action.
func resolveURL(raw, origin string) (string, bool) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//") {
		if origin == "" {
			return "", false
		}
		s = origin + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	return s, true
}
