package site

import (
	"context"
	"sort"
	"strings"
	"sync"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/microdata"
	"github.com/sky-valley/pagelike/internal/store"
)

// ParsedDoc is an immutable parse of a stored markup document. Callers must
// not mutate Root; clone it (dom.Clone) or reparse before editing.
type ParsedDoc struct {
	Path    string
	Version int64
	ETag    string
	Type    string
	Root    *html.Node
	Items   []*microdata.Item // every item in the document, document order
}

// Snapshot is an immutable view of host-wide derived data at one generation.
type Snapshot struct {
	configuration *Snapshot
	Generation    int64
	settingsRev   int64                 // settings revision the snapshot was built with
	Docs          map[string]*ParsedDoc // markup documents by path
	Paths         []string              // sorted markup document paths
	Policy        *authz.Policy
	byType        map[string][]TypedItem

	extMu sync.Mutex
	ext   map[string]any // lazily derived structures keyed by owner package
}

// Configuration excludes mutable participant documents on hosted sites. Data
// remains queryable through the original snapshot but cannot define authority,
// schemas, or server reactions.
func (s *Snapshot) Configuration() *Snapshot {
	if s.configuration != nil {
		return s.configuration
	}
	return s
}

// TypedItem is an item located in a document.
type TypedItem struct {
	Path string
	Item *microdata.Item
}

// ItemsOfType returns every item on the host declaring itemtype t.
func (s *Snapshot) ItemsOfType(t string) []TypedItem { return s.byType[t] }

// Types returns every itemtype that occurs on the host.
func (s *Snapshot) Types() []string {
	out := make([]string, 0, len(s.byType))
	for t := range s.byType {
		out = append(out, t)
	}
	return out
}

// Ext returns a lazily computed structure derived from this snapshot, so
// packages (schema, reactions, …) can cache their own indexes per generation.
func (s *Snapshot) Ext(key string, build func(*Snapshot) any) any {
	s.extMu.Lock()
	defer s.extMu.Unlock()
	if v, ok := s.ext[key]; ok {
		return v
	}
	v := build(s)
	s.ext[key] = v
	return v
}

// Index maintains the current Snapshot.
type Index struct {
	site *Site
	mu   sync.Mutex
	cur  *Snapshot
	docs map[string]*ParsedDoc // parse cache by path (validated by version)
}

func newIndex(s *Site) *Index { return &Index{site: s, docs: map[string]*ParsedDoc{}} }

// IsMarkup reports whether a content type is parsed (HTML or XML family).
func IsMarkup(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	return ct == "text/html" || ct == "application/xhtml+xml" || IsXML(ct)
}

// IsXML reports whether a content type belongs to the XML family.
func IsXML(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	return ct == "application/xml" || ct == "text/xml" || (strings.HasSuffix(ct, "+xml") && ct != "application/xhtml+xml")
}

// Get returns the snapshot for the current generation.
func (ix *Index) Get(ctx context.Context) (*Snapshot, error) {
	// Authorization and schema discovery always use the current authored rules,
	// including requests rendering an older immutable published document.
	version := store.PublishedVersion(ctx)
	ctx = store.WithPublishedVersion(ctx, "")
	gen, err := ix.site.Store.Generation(ctx)
	if err != nil {
		return nil, err
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	rev := ix.site.SettingsRevision()
	if ix.cur != nil && ix.cur.Generation == gen && ix.cur.settingsRev == rev {
		return ix.view(ctx, ix.cur, version)
	}
	metas, err := ix.site.Store.List(ctx, "/", false)
	if err != nil {
		return nil, err
	}
	snap := &Snapshot{Generation: gen, settingsRev: rev, Docs: map[string]*ParsedDoc{}, byType: map[string][]TypedItem{}, ext: map[string]any{}}
	pol := &authz.Policy{DefaultGet: ix.site.Settings().DefaultGet != "deny"}
	live := map[string]bool{}
	for _, m := range metas {
		if m.IsBlob() || !IsMarkup(m.ContentType) {
			continue
		}
		live[m.Path] = true
		pd := ix.docs[m.Path]
		if pd == nil || pd.Version != m.Version {
			d, err := ix.site.Store.Get(ctx, m.Path)
			if err != nil {
				continue // deleted concurrently; next generation will reconcile
			}
			pd = Parse(d)
			ix.docs[m.Path] = pd
		}
		snap.Docs[m.Path] = pd
		snap.Paths = append(snap.Paths, m.Path)
		for _, it := range pd.Items {
			for _, t := range it.Types {
				snap.byType[t] = append(snap.byType[t], TypedItem{Path: pd.Path, Item: it})
			}
		}
		if !ix.site.Settings().Hosted || !store.LivePath(pd.Path) {
			rules, groups := authz.ExtractRules(pd.Path, pd.Root)
			pol.Rules = append(pol.Rules, rules...)
			pol.Groups = append(pol.Groups, groups...)
		}
	}
	for p := range ix.docs {
		if !live[p] {
			delete(ix.docs, p)
		}
	}
	if ix.site.Settings().Hosted {
		c := &Snapshot{Generation: gen, settingsRev: rev, Docs: map[string]*ParsedDoc{}, byType: map[string][]TypedItem{}, ext: map[string]any{}}
		for _, p := range snap.Paths {
			if store.LivePath(p) {
				continue
			}
			pd := snap.Docs[p]
			c.Docs[p] = pd
			c.Paths = append(c.Paths, p)
			for _, it := range pd.Items {
				for _, typ := range it.Types {
					c.byType[typ] = append(c.byType[typ], TypedItem{Path: p, Item: it})
				}
			}
		}
		snap.configuration = c
	}
	pol.RunHooks(snap.Configuration()) // schema: Group subtypes, rule subtypes (authz.Hook)
	snap.Policy = pol
	snap.Configuration().Policy = pol
	ix.cur = snap
	return ix.view(ctx, snap, version)
}

// Composition sees one authored version in includes, bindings and host queries.
// Its policy, schema and reactions still come from current authored rules.
func (ix *Index) view(ctx context.Context, current *Snapshot, version string) (*Snapshot, error) {
	if version == "" {
		return current, nil
	}
	docs, err := ix.site.Store.PublishedDocuments(ctx, version)
	if err != nil {
		return nil, err
	}
	view := &Snapshot{Generation: current.Generation, settingsRev: current.settingsRev, Policy: current.Policy, configuration: current.Configuration(), Docs: map[string]*ParsedDoc{}, byType: map[string][]TypedItem{}, ext: map[string]any{}}
	add := func(p *ParsedDoc) {
		view.Docs[p.Path] = p
		view.Paths = append(view.Paths, p.Path)
		for _, it := range p.Items {
			for _, typ := range it.Types {
				view.byType[typ] = append(view.byType[typ], TypedItem{Path: p.Path, Item: it})
			}
		}
	}
	for _, d := range docs {
		if !d.IsBlob() && IsMarkup(d.ContentType) {
			add(Parse(d))
		}
	}
	for _, p := range current.Paths {
		if store.LivePath(p) {
			add(current.Docs[p])
		}
	}
	sort.Strings(view.Paths)
	return view, nil
}

// ParseMarkup parses a stored markup document of content type ct: XML-family
// types through xmldom (decision 0003 §3), everything else (HTML, XHTML)
// through the HTML parser. The trees have the same node type, so selectors,
// microdata and composition work on both.
func ParseMarkup(ct string, body []byte) (*html.Node, error) {
	if IsXML(ct) {
		return dom.ParseXML(body)
	}
	return dom.Parse(body)
}

// Parse parses a stored markup document into an immutable ParsedDoc. A
// document that does not parse (malformed XML) indexes as empty.
func Parse(d *store.Document) *ParsedDoc {
	root, err := ParseMarkup(d.ContentType, d.Body)
	if err != nil || root == nil {
		root = &html.Node{Type: html.DocumentNode}
	}
	return &ParsedDoc{Path: d.Path, Version: d.Version, ETag: d.ETag, Type: d.ContentType, Root: root, Items: microdata.AllItems(root)}
}
