package compose

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/budget"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/liquid"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

// Engine composes pages. One per process; safe for concurrent use.
type Engine struct {
	// Liquid renders p:template hosts.
	Liquid *liquid.Engine
	// JS runs j: bindings and JavaScript methods; nil fails them with 501.
	JS JSRunner
	// Methods dispatches method elements and attributes; nil means
	// SchemaMethods.
	Methods MethodDispatcher
}

// NewEngine returns an engine with a Liquid renderer and the default
// method dispatcher.
func NewEngine() *Engine { return &Engine{Liquid: liquid.New(liquid.Options{AutoEscape: true})} }

var (
	defaultEngine = NewEngine()
	jsRunner      atomic.Pointer[JSRunner]
	dispatcher    atomic.Pointer[MethodDispatcher]
)

// Default returns the process-wide engine the server uses.
func Default() *Engine { return defaultEngine }

// SetJSRunner installs the server JavaScript runtime (internal/jsrt) for
// every engine that has no runner of its own.
func SetJSRunner(r JSRunner) { jsRunner.Store(&r) }

// SetMethodDispatcher installs the schema package's method dispatcher for
// every engine that has none of its own.
func SetMethodDispatcher(d MethodDispatcher) { dispatcher.Store(&d) }

func (e *Engine) js() JSRunner {
	if e.JS != nil {
		return e.JS
	}
	if p := jsRunner.Load(); p != nil {
		return *p
	}
	return nil
}

func (e *Engine) methods() MethodDispatcher {
	if e.Methods != nil {
		return e.Methods
	}
	if p := dispatcher.Load(); p != nil && *p != nil {
		return *p
	}
	return SchemaMethods{}
}

func init() { server.Extend(Register) }

// Register wires composition into a server: the public plane's Composer
// and Router, selector-write routing (write-through, transient elements,
// route URLs) and templated resource creation.
func Register(s *server.Server) {
	engine.EventAudience = includers
	e := defaultEngine
	s.Public.Compose = e.Compose
	s.Public.Route = Route
	engine.ComposeWrite = e.composeWrite
	engine.TemplateCreate = e.templateCreate
	engine.SelectorFunctions = ExpandSelector
}

// Compose implements engine.Composer: the composed view of a stored
// document for one read (GET, HEAD, edge QUERY, JSON-LD).
func (e *Engine) Compose(ctx context.Context, s *site.Site, snap *site.Snapshot, doc *store.Document, root *html.Node, op *engine.ReadOp) (*engine.Composed, error) {
	if op.RoutePath != "" && op.Range.HasSelector() && (op.Method == http.MethodGet || op.Method == http.MethodHead) {
		// Routes resolve for whole-document reads only (R-COMP-107).
		return nil, errdoc.New(http.StatusNotFound, "NotFound", "%s was not found", op.Path)
	}
	req := &Request{Method: op.Method, Path: op.Path, RawQuery: op.RawQuery, Query: op.Query, Header: op.Header,
		Host: op.Host, Principal: op.Principal}
	if op.Target != "" {
		req.Path = op.Target
	}
	if op.RoutePath != "" {
		req.Params = routeParams(op.RoutePath, op.Path)
	}
	res, err := e.compose(ctx, s, snap, doc, root, req, options{})
	if err != nil {
		return nil, err
	}
	comp := &engine.Composed{Root: root, Private: res.private, Header: http.Header{}}
	for k, v := range res.header {
		comp.Header[k] = v
	}
	if res.changed {
		comp.Changed = true
		comp.Body = []byte(res.text)
		if comp.Root, err = site.ParseMarkup(doc.ContentType, comp.Body); err != nil {
			return nil, compositionError("composed %s does not parse: %v", doc.Path, err)
		}
		if res.xml {
			// A composed XML document is served as well-formed XML in
			// PageLove's dialect: empty elements self-close, every xmlns
			// declaration stays (R-COMP-126). Unchanged XML is served as
			// stored (R-COMP-128).
			comp.Body = dom.Render(comp.Root)
		} else {
			comp.Fragment = sourceFragments(comp.Root, res.text)
		}
	}
	if len(req.Params) > 0 {
		pairs := make([]string, len(req.Params))
		for i, p := range req.Params {
			pairs[i] = p.Name + "=" + p.Value
		}
		comp.Header.Set("Route-Parameters", strings.Join(pairs, ", "))
	}
	// Shareable composed pages carry the short shared-cache floor PageLove
	// was observed sending (public, max-age=5); pages that read
	// per-requester data are private (R-COMP-150). Fragments of shareable
	// pages carry no Cache-Control (R-RW-102, observed live).
	if res.changed && !res.private && !op.Range.HasSelector() {
		comp.Header.Set("Cache-Control", "public, max-age=5")
	}
	return comp, nil
}

// options select composition variants.
type options struct {
	// create composes a template for storage (templated creation,
	// R-COMP-143): xmlns:* declarations are kept (stripping is a response
	// transformation), and the read-time directives p:paginate and
	// p:transient stay in the stored document instead of being applied.
	create bool
	// track records provenance anchors (selector writes).
	track bool
}

// result is the outcome of composing a document.
type result struct {
	text    string
	changed bool
	private bool
	header  http.Header
	xml     bool
	// provenance of the composed view, for selector writes
	anchors []anchor
	trans   []transRec
	docPath string // stored path of the composed document
}

// anchor records that the element starting at off in the composed text is
// node, an element of the stored document at path whose parse is rooted at
// root. Generated elements have no anchor (R-COMP-90).
type anchor struct {
	off        int
	path       string
	node, root *html.Node
}

// idx is the anchored element's position: child indices from the
// document node, valid in any parse of the same stored bytes.
func (a *anchor) idx() []int { return indexPath(a.root, a.node) }

// compose composes doc (stored at doc.Path; for a route, the template) for
// req. root, when non-nil, is a parse of doc.Body that may be reused.
func (e *Engine) compose(ctx context.Context, s *site.Site, snap *site.Snapshot, doc *store.Document, root *html.Node, req *Request, o options) (*result, error) {
	res := &result{text: string(doc.Body), xml: site.IsXML(doc.ContentType), header: http.Header{}, docPath: doc.Path}
	if pd := snap.Docs[doc.Path]; pd != nil && pd.Version == doc.Version && pd.ETag == doc.ETag {
		root = pd.Root
	}
	if root == nil {
		var err error
		if root, err = site.ParseMarkup(doc.ContentType, doc.Body); err != nil {
			return res, nil // unparsable XML: served as stored
		}
	}
	if !mayCompose(root) {
		return res, nil
	}
	r, err := e.documentRegion(ctx, s, snap, doc)
	var ed *errdoc.Error
	if errors.As(err, &ed) {
		return nil, ed
	}
	if err != nil || r == nil {
		return res, nil // does not parse (malformed XML): served as stored
	}
	c := &composer{eng: e, ctx: ctx, site: s, snap: snap, req: req, doc: r, docPath: doc.Path, xml: r.xml,
		create: o.create, track: o.track, budget: budget.From(ctx)}
	if err := c.run(); err != nil {
		return nil, err
	}
	out, err := c.finish(res)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// documentRegion returns the region of the requested document, sharing
// the snapshot's parse when it is of the same bytes.
func (e *Engine) documentRegion(ctx context.Context, s *site.Site, snap *site.Snapshot, doc *store.Document) (*region, error) {
	if pd := snap.Docs[doc.Path]; pd != nil && pd.Version == doc.Version && pd.ETag == doc.ETag {
		return cachedRegion(snap, doc.Path, func() (*region, error) {
			return newDocRegion(doc.Body, pd.Type, pd, true)
		})
	}
	r, err := newDocRegion(doc.Body, doc.ContentType, nil, false)
	if err != nil {
		return nil, err
	}
	r.path = doc.Path
	return r, nil
}

// mayCompose reports whether a document has anything composition acts
// on: a prefixed element or attribute (directives, xmlns declarations).
// Documents without any are served as stored.
func mayCompose(root *html.Node) bool {
	found := false
	dom.Walk(root, func(n *html.Node) bool {
		if found {
			return false
		}
		if n.Type != html.ElementNode {
			return true
		}
		if strings.Contains(n.Data, ":") {
			found = true
			return false
		}
		for _, a := range n.Attr {
			if strings.Contains(a.Key, ":") {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// sourceFragments returns the elements of a composed HTML page as they are
// written in its text (template output keeps its bytes in fragment reads
// too). Spans are computed on first use.
func sourceFragments(root *html.Node, text string) func(*html.Node) (string, bool) {
	var once sync.Once
	var spans dom.Spans
	return func(n *html.Node) (string, bool) {
		once.Do(func() { spans = dom.ComputeSpans(root, text) })
		sp, ok := spans.By[n]
		if !ok || !spans.Clean || !(sp.ExplicitEnd || sp.NoContent) || sp.Start >= sp.End || sp.End > len(text) {
			return "", false
		}
		return text[sp.Start:sp.End], true
	}
}
