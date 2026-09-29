package compose

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
	"github.com/sky-valley/pagelike/internal/xmldom"
)

// provKind is the provenance of the elements of a region (R-COMP-90).
type provKind int

const (
	// provNative: elements of the requested stored document.
	provNative provKind = iota
	// provProjected: elements of another stored document, spliced by an
	// include or a stamp; writes route to that document.
	provProjected
	// provGenerated: template output, constructed method/binding results,
	// the Request Document; not writable.
	provGenerated
)

// region is a piece of markup source that composition walks: a stored
// document, a template's rendered output, a constructed value's
// serialization. Composition copies the region's bytes to the output,
// splicing in directive results, so everything it does not change stays
// verbatim (decision 0003 §6).
type region struct {
	text  string
	root  *html.Node // document node, or a holder for fragment nodes
	spans dom.Spans
	xml   bool
	kind  provKind
	path  string // stored document path (native, projected)
	// snapRoot is the snapshot tree this region's structure corresponds to:
	// elements the site graph hands out (Sessel queries, bindings) are
	// nodes of the snapshot tree and are mapped into the region by their
	// position.
	snapRoot *html.Node
	// tplOut marks template output: nested *:template attributes are
	// stripped, not rendered again (R-LIQ-7).
	tplOut bool
	// context is the element a fragment's top-level nodes were parsed in.
	context *html.Node
	// reqDoc marks the Request Document (reading it makes the page private).
	reqDoc bool
	// frag marks a fragment (root is a holder of top-level nodes).
	frag bool
	// ns are the namespace declarations an XML fragment was parsed under.
	ns map[string]string
}

// node maps an element of the snapshot tree (or of this region) into the
// region.
func (r *region) node(n *html.Node) *html.Node {
	if _, ok := r.spans.By[n]; ok || r.snapRoot == nil || r.snapRoot == r.root {
		return n
	}
	idx := indexPath(r.snapRoot, n)
	if idx == nil {
		return nil
	}
	return nodeAt(r.root, idx)
}

// indexPath lists child indices from root down to n (nil when n is not
// under root).
func indexPath(root, n *html.Node) []int {
	var idx []int
	for x := n; x != root; x = x.Parent {
		if x == nil || x.Parent == nil {
			return nil
		}
		i := 0
		for c := x.Parent.FirstChild; c != x; c = c.NextSibling {
			i++
		}
		idx = append(idx, i)
	}
	for i, j := 0, len(idx)-1; i < j; i, j = i+1, j-1 {
		idx[i], idx[j] = idx[j], idx[i]
	}
	if idx == nil {
		idx = []int{}
	}
	return idx
}

// nodeAt follows child indices from root.
func nodeAt(root *html.Node, idx []int) *html.Node {
	cur := root
	for _, i := range idx {
		if cur == nil {
			return nil
		}
		c := cur.FirstChild
		for ; c != nil && i > 0; i-- {
			c = c.NextSibling
		}
		cur = c
	}
	return cur
}

// docRegions caches the regions of stored documents per snapshot.
type docRegions struct {
	mu sync.Mutex
	m  map[string]*region
}

const regionsKey = "compose.regions"

func regionsOf(snap *site.Snapshot) *docRegions {
	return snap.Ext(regionsKey, func(*site.Snapshot) any { return &docRegions{m: map[string]*region{}} }).(*docRegions)
}

// docRegion returns the region of the stored markup document at path
// (nil when there is none). Its tree is the snapshot's own parse whenever
// the stored bytes are the ones the snapshot parsed.
func docRegion(ctx context.Context, s *site.Site, snap *site.Snapshot, path string) (*region, error) {
	pd := snap.Docs[path]
	if pd == nil {
		return nil, nil
	}
	return cachedRegion(snap, path, func() (*region, error) {
		d, err := s.Store.Get(ctx, path)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return newDocRegion(d.Body, pd.Type, pd, d.Version == pd.Version && d.ETag == pd.ETag)
	})
}

// cachedRegion returns the snapshot's region for path, building it once.
func cachedRegion(snap *site.Snapshot, path string, build func() (*region, error)) (*region, error) {
	cache := regionsOf(snap)
	cache.mu.Lock()
	r := cache.m[path]
	cache.mu.Unlock()
	if r != nil {
		return r, nil
	}
	r, err := build()
	if err != nil || r == nil {
		return r, err
	}
	r.path = path
	cache.mu.Lock()
	if prev := cache.m[path]; prev != nil {
		r = prev
	} else {
		cache.m[path] = r
	}
	cache.mu.Unlock()
	return r, nil
}

// newDocRegion builds the region of a stored document's bytes. same reports
// that pd (the snapshot's parse) was parsed from exactly these bytes.
func newDocRegion(body []byte, ct string, pd *site.ParsedDoc, same bool) (*region, error) {
	r := &region{text: string(body), xml: site.IsXML(ct), kind: provNative}
	if pd != nil {
		r.snapRoot = pd.Root
	}
	if r.xml {
		root, sp, err := dom.ParseXMLWithSpans(body)
		if err != nil {
			return nil, err
		}
		r.root, r.spans = root, sp
	} else {
		if same && pd != nil {
			r.root = pd.Root
		} else {
			root, err := dom.Parse(body)
			if err != nil {
				return nil, err
			}
			r.root = root
		}
		r.spans = dom.ComputeSpans(r.root, r.text)
	}
	if err := ensureWalkable(r); err != nil {
		return nil, err
	}
	return r, nil
}

// walkable reports whether a region's element spans can guide the walk:
// every element with a span lies inside its parent's content, after its
// previous sibling. Elements without a span (implied by the parser, or
// parser clones of formatting elements) are walked through. This is weaker
// than dom.Spans.Clean on purpose: a tag the tokenizer saw in text that the
// parser kept as text (a raw-text template host's output) or a
// reconstructed formatting element does not disturb the source order.
func walkable(r *region) bool {
	var check func(parent *html.Node, pos, hi int) (int, bool)
	check = func(parent *html.Node, pos, hi int) (int, bool) {
		for ch := parent.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type != html.ElementNode {
				continue
			}
			sp, ok := r.spans.By[ch]
			if !ok {
				if !impliedKind(ch) && !formattingElement(ch) {
					return 0, false // an element the tokenizer could not place
				}
				var fine bool
				if pos, fine = check(ch, pos, hi); !fine {
					return 0, false
				}
				continue
			}
			if sp.Start < pos || sp.End > hi || sp.Start > sp.InnerStart || sp.InnerStart > sp.InnerEnd || sp.InnerEnd > sp.End {
				return 0, false
			}
			if !sp.NoContent {
				if _, fine := check(ch, sp.InnerStart, sp.InnerEnd); !fine {
					return 0, false
				}
			}
			pos = sp.End
		}
		return pos, true
	}
	_, ok := check(r.root, 0, len(r.text))
	return ok
}

// ensureWalkable makes a region walkable: a region whose elements cannot
// be correlated with their source (foster parenting, misnested formatting
// elements) is replaced by its serialization, which parses back to the same
// tree with clean spans. A region that still cannot be walked fails
// composition clearly rather than losing content.
func ensureWalkable(r *region) error {
	if walkable(r) {
		return nil
	}
	switch {
	case r.frag:
		var b strings.Builder
		for c := r.root.FirstChild; c != nil; c = c.NextSibling {
			b.WriteString(dom.StorageHTML(c))
		}
		if nr, err := fragmentRegion(b.String(), r.context, r.xml, r.ns); err == nil {
			r.text, r.root, r.spans = nr.text, nr.root, nr.spans
		}
	case r.xml:
		text := string(dom.RenderStorage(r.root, nil))
		if root, sp, err := dom.ParseXMLWithSpans([]byte(text)); err == nil {
			r.text, r.root, r.spans = text, root, sp
		}
	default:
		text := string(dom.RenderStorage(r.root, dom.ImpliedFor(r.root, r.text)))
		if root, err := dom.Parse([]byte(text)); err == nil {
			r.text, r.root, r.spans = text, root, dom.ComputeSpans(root, text)
		}
	}
	if !walkable(r) {
		what := "generated markup"
		if r.path != "" {
			what = r.path
		}
		return compositionError("%s cannot be composed: its elements do not correspond to its source text", what)
	}
	return nil
}

// fragmentRegion parses text as the content of context (the element the
// markup is spliced into; nil means a <body>), recording source spans.
func fragmentRegion(text string, context *html.Node, xml bool, ns map[string]string) (*region, error) {
	r := &region{text: text, xml: xml, kind: provGenerated, context: context, frag: true}
	if xml {
		return xmlFragmentRegion(r, ns)
	}
	if context == nil || context.Type != html.ElementNode {
		context = &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	}
	trimmed := dom.TrimHTMLSpace(text)
	lead := strings.Index(text, trimmed)
	if trimmed == "" {
		lead = 0
	}
	holder := &html.Node{Type: html.DocumentNode}
	if trimmed != "" {
		nodes, err := dom.ParseFragment(trimmed, context)
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			holder.AppendChild(n)
		}
	}
	sp := dom.ComputeSpans(holder, trimmed)
	if lead > 0 {
		for n, s := range sp.By {
			s.Start += lead
			s.InnerStart += lead
			s.InnerEnd += lead
			s.End += lead
			sp.By[n] = s
		}
	}
	r.root, r.spans = holder, sp
	return r, nil
}

const xmlFragmentRoot = "pagelike-fragment"

// xmlFragmentRegion parses r.text as XML content under the namespace
// declarations ns (prefix → URI, "" the default namespace).
func xmlFragmentRegion(r *region, ns map[string]string) (*region, error) {
	r.ns = ns
	var b strings.Builder
	b.WriteString("<" + xmlFragmentRoot)
	prefixes := make([]string, 0, len(ns))
	for p := range ns {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)
	for _, p := range prefixes {
		name := "xmlns"
		if p != "" {
			name += ":" + p
		}
		b.WriteString(" " + name + `="` + escapeAttr(ns[p]) + `"`)
	}
	b.WriteString(">")
	off := b.Len()
	b.WriteString(r.text)
	b.WriteString("</" + xmlFragmentRoot + ">")
	doc, xs, err := xmldom.ParseWithSpans([]byte(b.String()), xmldom.Options{})
	if err != nil {
		return nil, err
	}
	var wrap *html.Node
	for c := doc.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			wrap = c
			break
		}
	}
	if wrap == nil || wrap.Data != xmlFragmentRoot {
		return nil, errors.New("xml fragment did not parse as element content")
	}
	holder := &html.Node{Type: html.DocumentNode}
	for c := wrap.FirstChild; c != nil; {
		next := c.NextSibling
		wrap.RemoveChild(c)
		holder.AppendChild(c)
		c = next
	}
	sp := dom.Spans{By: map[*html.Node]dom.Span{}, Clean: xs.Clean}
	for n, s := range xs.By {
		if n == wrap {
			continue
		}
		sp.By[n] = dom.Span{Start: s.Start - off, InnerStart: s.InnerStart - off, InnerEnd: s.InnerEnd - off,
			End: s.End - off, ExplicitEnd: true, NoContent: s.SelfClosing}
	}
	r.root, r.spans = holder, sp
	return r, nil
}

// impliedKind reports elements the HTML parser may create without a start
// tag; they have no span and are walked through.
func impliedKind(n *html.Node) bool {
	if n.Namespace != "" {
		return false
	}
	switch n.DataAtom {
	case atom.Html, atom.Head, atom.Body, atom.Tbody, atom.Colgroup:
		return true
	}
	return false
}

// formattingElement reports the HTML formatting elements, which the parser
// may clone without a start tag (reconstruction of active formatting
// elements, the adoption agency); like implied elements they are walked
// through.
func formattingElement(n *html.Node) bool {
	if n.Namespace != "" {
		return false
	}
	switch n.DataAtom {
	case atom.A, atom.B, atom.Big, atom.Code, atom.Em, atom.Font, atom.I, atom.Nobr,
		atom.S, atom.Small, atom.Strike, atom.Strong, atom.Tt, atom.U:
		return true
	}
	return false
}
