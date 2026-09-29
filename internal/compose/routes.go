package compose

import (
	"strings"

	"github.com/sky-valley/pagelike/internal/site"
)

// Parameterized routes (docs/spec/composing.md §15). A stored document
// whose path has segments beginning with ':' is a route template: a
// directory segment ":name" captures one request segment; a filename
// segment ":name<suffix>" (":slug.html") captures the part of the request
// filename before <suffix>. The template with the most literal segments
// wins; ties go to the smaller stored path (R-COMP-102, decision C-11).
// Routes resolve only when no literal document exists at the request path
// (httpapi calls Route only then).

type routeSeg struct {
	literal string
	param   string // "" for a literal segment
	suffix  string
}

type route struct {
	path     string
	segs     []routeSeg
	literals int
}

type routeTable struct{ routes []route }

const routesKey = "compose.routes"

func routesOf(snap *site.Snapshot) *routeTable {
	return snap.Ext(routesKey, func(s *site.Snapshot) any {
		t := &routeTable{}
		for _, p := range s.Paths {
			if rt, ok := parseRoute(p); ok {
				t.routes = append(t.routes, rt)
			}
		}
		return t
	}).(*routeTable)
}

// parseRoute recognises a route template path.
func parseRoute(p string) (route, bool) {
	if !strings.Contains(p, "/:") {
		return route{}, false
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	rt := route{path: p}
	params := false
	for _, s := range parts {
		if strings.HasPrefix(s, ":") && len(s) > 1 {
			name, suffix := s[1:], ""
			if i := strings.IndexByte(name, '.'); i >= 0 {
				name, suffix = name[:i], name[i:]
			}
			if name == "" {
				rt.segs = append(rt.segs, routeSeg{literal: s})
				rt.literals++
				continue
			}
			rt.segs = append(rt.segs, routeSeg{param: name, suffix: suffix})
			params = true
			continue
		}
		rt.segs = append(rt.segs, routeSeg{literal: s})
		rt.literals++
	}
	return rt, params
}

// match captures the parameters of reqPath, or reports no match.
func (rt route) match(reqPath string) ([]Param, bool) {
	parts := strings.Split(strings.TrimPrefix(reqPath, "/"), "/")
	if len(parts) != len(rt.segs) {
		return nil, false
	}
	var out []Param
	for i, s := range rt.segs {
		seg := parts[i]
		if s.param == "" {
			if seg != s.literal {
				return nil, false
			}
			continue
		}
		if !strings.HasSuffix(seg, s.suffix) || len(seg) == len(s.suffix) {
			return nil, false
		}
		out = append(out, Param{Name: s.param, Value: seg[:len(seg)-len(s.suffix)]})
	}
	return out, true
}

// resolveRoute finds the winning template for a request path.
func resolveRoute(snap *site.Snapshot, reqPath string) (string, []Param, bool) {
	var best *route
	var bestParams []Param
	for i := range routesOf(snap).routes {
		rt := &routesOf(snap).routes[i]
		ps, ok := rt.match(reqPath)
		if !ok {
			continue
		}
		if best == nil || rt.literals > best.literals || (rt.literals == best.literals && rt.path < best.path) {
			best, bestParams = rt, ps
		}
	}
	if best == nil {
		return "", nil, false
	}
	return best.path, bestParams, true
}

// Route implements httpapi.Router: the stored template serving a request
// path that has no literal document, and its captures (percent-decoded:
// the request path is).
func Route(snap *site.Snapshot, reqPath string) (string, map[string]string, bool) {
	p, params, ok := resolveRoute(snap, reqPath)
	if !ok {
		return "", nil, false
	}
	m := make(map[string]string, len(params))
	for _, x := range params {
		m[x.Name] = x.Value
	}
	return p, m, true
}

// routeParams recomputes the ordered captures of reqPath under template.
func routeParams(template, reqPath string) []Param {
	rt, ok := parseRoute(template)
	if !ok {
		return nil
	}
	ps, _ := rt.match(reqPath)
	return ps
}
