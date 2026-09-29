package compose

import (
	"path"
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/selector"
	"github.com/sky-valley/pagelike/internal/site"
)

// includers returns the pages that include a fragment of doc through an
// include element: explicitly (a resource glob matching doc) or globally
// (no resource, and the include's selector matches something in doc). Live
// PageLove delivers doc's change events to those pages' subscribers too.
func includers(snap *site.Snapshot, doc string) []string {
	idx := snap.Ext("compose.includers", func(s *site.Snapshot) any { return buildIncludes(s) }).(map[string][]includeRef)
	var out []string
	for page, refs := range idx {
		if page == doc {
			continue
		}
		for _, ref := range refs {
			if ref.matches(snap, doc) {
				out = append(out, page)
				break
			}
		}
	}
	return out
}

type includeRef struct {
	globs []string
	sel   *selector.Selector
}

func (r includeRef) matches(snap *site.Snapshot, doc string) bool {
	if len(r.globs) > 0 {
		return matchesAny(r.globs, doc)
	}
	pd := snap.Docs[doc]
	return pd != nil && r.sel != nil && r.sel.MatchFirst(pd.Root) != nil
}

// buildIncludes lists every include element on the host, per page.
func buildIncludes(s *site.Snapshot) map[string][]includeRef {
	out := map[string][]includeRef{}
	for _, p := range s.Paths {
		pd := s.Docs[p]
		if pd == nil {
			continue
		}
		dom.Walk(pd.Root, func(n *html.Node) bool {
			if n.Type != html.ElementNode || !strings.HasSuffix(n.Data, ":include") {
				return true
			}
			ref := includeRef{}
			if res, ok := html5Attr(n, "resource"); ok {
				for _, g := range strings.Fields(res) {
					if !strings.HasPrefix(g, "/") {
						g = path.Join(path.Dir(p), g)
					}
					ref.globs = append(ref.globs, g)
				}
			}
			if src, ok := html5Attr(n, "selector"); ok {
				ref.sel, _ = selector.Compile(src)
			}
			out[p] = append(out[p], ref)
			return true
		})
	}
	return out
}
