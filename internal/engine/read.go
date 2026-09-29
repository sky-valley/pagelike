package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/microdata"
	"github.com/sky-valley/pagelike/internal/selector"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

// ReadOp is a GET/HEAD request for a markup document.
type ReadOp struct {
	Plane     Plane
	Method    string // GET or HEAD
	Path      string
	Range     Range
	Accept    string
	Principal *identity.Principal
	Header    http.Header
	Query     url.Values
	RawQuery  string // the query string as received (order matters to pagination links)
	Host      string
	Params    map[string]string // parameterized-route captures
	RoutePath string            // stored path of the route template, if any
	// Target is the normalized request path as sent: a directory keeps its
	// trailing slash (Path is then its index document). Set for QUERY.
	Target string
}

// Composed is the output of page composition.
type Composed struct {
	Root    *html.Node
	Changed bool // composition altered the stored markup
	Private bool // output depends on the requester (Cache-Control: private)
	Status  int  // status override requested by the page (0 = default)
	Header  http.Header
	// Body is the served markup when Changed. Composition splices its
	// changes into the stored source, so untouched bytes stay verbatim
	// (decision 0003 §6); nil means serialize Root.
	Body []byte
	// Fragment, when set, returns an element of Root as it is written in
	// Body, for single-element selector reads (the fragment ETag stays the
	// hash of the element's serialization, which writes compare against).
	Fragment func(n *html.Node) (string, bool)
}

// Composer renders a stored document for a request (includes, bindings,
// templates, transients…). Nil means documents are served as stored.
type Composer func(ctx context.Context, s *site.Site, snap *site.Snapshot, doc *store.Document, root *html.Node, op *ReadOp) (*Composed, error)

// ReadResult is the outcome of a read.
type ReadResult struct {
	Status int
	Header http.Header
	Body   []byte
}

// Vary values PageLove sends (live 2026-09-28; R-RW-37 as reconciled):
// document reads vary on Host, Range and Accept, all-matches reads also on
// Paginate, and writes, blobs, XML documents and errors on Host and Range.
const (
	ReadVary     = "Host, Range, Accept"
	AllMatchVary = "Host, Range, Accept, Paginate"
	WriteVary    = "Host, Range"
)

// Accept-Ranges advertisements. HTML document responses advertise the
// documented "selector, bytes" (docs, Accept-Ranges; entries is supported
// but never advertised). Live PageLove sends none on HTML reads and "bytes"
// on JSON-LD, all-matches and XML answers; pagelike keeps the documented
// superset (compat decision C-5, reviewed in docs/compat/decisions.md) and
// sends "bytes" where PageLove serves like a blob (XML documents).
const (
	DocumentRanges = "selector, bytes"
	BytesRanges    = "bytes"
)

func (op *ReadOp) authzReq(method string) authz.Request {
	return authz.Request{Principal: op.Principal, Method: method, Path: op.Path, Header: op.Header, Query: op.Query}
}

// AuthorizeRead checks read access to the document (target nil) or to one
// of its elements.
func AuthorizeRead(snap *site.Snapshot, op *ReadOp, target *html.Node) bool {
	if op.Plane == Authoring {
		return true
	}
	return snap.Policy.Decide(op.authzReq(op.Method), target).Allowed
}

// CanRead reports whether any rule could let the requester read some part
// of the document; a read that fails it is refused before the document's
// existence is revealed (R-RW-126).
func CanRead(snap *site.Snapshot, op *ReadOp) bool {
	return op.Plane == Authoring || CanGrant(snap.Policy, op.authzReq(op.Method))
}

// selectorOnNonHTML is the read path's refusal of a selector range on a
// blob or XML document (live 2026-09-28: PageLove selects only in HTML).
func selectorOnNonHTML(doc *store.Document) *errdoc.Error {
	return errdoc.Read(http.StatusUnprocessableEntity, "InvalidPath", "Invalid path: Selector operations require HTML documents, but %s has content type %s", doc.Path, doc.ContentType)
}

// SelectorOnNonHTML is selectorOnNonHTML for the transports (blobs).
func SelectorOnNonHTML(doc *store.Document) error { return selectorOnNonHTML(doc) }

// InvalidReadSelector is the read path's answer to a selector range that is
// empty or does not parse: PageLove reports a 416 "HTML parsing error"
// (live 2026-09-28, superseding the 422 of R-RW-17 / C-10 for reads and
// css QUERY).
func InvalidReadSelector(err error) error {
	msg := err.Error()
	if e, ok := err.(*errdoc.Error); ok {
		msg = e.Message
	}
	return errdoc.Read(http.StatusRequestedRangeNotSatisfiable, "InvalidSelector", "HTML parsing error: Invalid CSS selector: %s", msg)
}

// jsonLD renders nodes' microdata as JSON-LD with every object's keys in
// sorted order ("@context", "@graph", "@id", "@type", then the properties),
// as PageLove emits it (live 2026-09-28); values and array order are
// unchanged.
func jsonLD(nodes []*html.Node) ([]byte, error) {
	js, err := microdata.JSONLD(nodes)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(js))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return js, nil
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return js, nil
	}
	out := bytes.TrimRight(b.Bytes(), "\n")
	if bytes.HasSuffix(js, []byte("\n")) {
		out = append(out, '\n')
	}
	return out, nil
}

// MissingDocument is the read path's 404 for path.
func MissingDocument(path string) error {
	return errdoc.Read(http.StatusNotFound, "NotFound", "Document not found: %s", path)
}

// ReadMarkup serves a stored markup document (whole or by selector) after
// composition, negotiating HTML, JSON-LD and (for selector reads) the
// all-matches multipart form (R-RW-20..39, as reconciled with live PageLove
// 2026-09-28). Conditional requests and byte ranges are the transport's
// job; it gets the ETag from the result.
func (e *Engine) ReadMarkup(ctx context.Context, s *site.Site, snap *site.Snapshot, doc *store.Document, op *ReadOp, compose Composer) (*ReadResult, error) {
	op.Range = op.Range.BindSnapshot(snap)
	if !op.Range.HasSelector() && !AuthorizeRead(snap, op, nil) {
		// A whole read needs the document-level grant; selector reads are
		// decided per element below.
		return nil, Denied(op.Principal, op.Path)
	}
	xml := site.IsXML(doc.ContentType)
	if xml && op.Range.HasSelector() && op.Plane == Public {
		return nil, selectorOnNonHTML(doc)
	}
	root, err := site.ParseMarkup(doc.ContentType, doc.Body)
	if err != nil {
		// Malformed XML is still served as stored.
		root = &html.Node{Type: html.DocumentNode}
	}
	comp := &Composed{Root: root}
	if compose != nil && op.Plane == Public {
		if comp, err = compose(ctx, s, snap, doc, root, op); err != nil {
			return nil, err
		}
	}
	// Fragments are served as stored while composition left the tree alone.
	var view *docView
	if !comp.Changed && comp.Root == root {
		view = newDocView(root, doc)
	}
	h := http.Header{}
	h.Set("Vary", ReadVary)
	h.Set("Accept-Ranges", DocumentRanges)
	if comp.Private {
		h.Set("Cache-Control", "private")
	}
	for k, v := range comp.Header {
		h[k] = v
	}
	status := http.StatusOK
	if comp.Status != 0 {
		status = comp.Status
	}

	if !op.Range.HasSelector() {
		lastMod := time.UnixMilli(doc.ModifiedMS).UTC().Format(http.TimeFormat)
		if !xml && Negotiate(op.Accept, false) == RepJSONLD {
			js, err := jsonLD([]*html.Node{comp.Root})
			if err != nil {
				return nil, err
			}
			h.Set("Content-Type", "application/ld+json; charset=utf-8")
			h.Set("Last-Modified", lastMod)
			h.Set("ETag", ETagOf(string(js)))
			return &ReadResult{Status: status, Header: h, Body: js}, nil
		}
		body, etag := doc.Body, doc.ETag
		if comp.Changed {
			// A composed page: weak tag, briefly cacheable, no
			// Last-Modified (live 2026-09-28); a page whose output depends
			// on the requester stays private (decisions.md
			// rw.reqdoc.auth-read-is-private).
			if body = comp.Body; body == nil { // spliced source bytes when composition kept them
				body = dom.Render(comp.Root)
			}
			etag = ComposedETag(body)
			if !comp.Private {
				h.Set("Cache-Control", "public, max-age=5")
			}
		} else {
			h.Set("Last-Modified", lastMod)
		}
		if xml {
			// XML documents are served like blobs (live 2026-09-28).
			h.Set("Vary", WriteVary)
			h.Set("Accept-Ranges", BytesRanges)
		}
		h.Set("Content-Type", contentTypeFor(doc.ContentType))
		h.Set("ETag", etag)
		return &ReadResult{Status: status, Header: h, Body: body}, nil
	}

	sel, err := op.Range.CheckSelector()
	if err != nil {
		return nil, InvalidReadSelector(err)
	}
	// Absence is reported only to those who may read the page (R-RW-127).
	absent := func() error {
		if op.Plane == Public && !AuthorizeRead(snap, op, nil) {
			return Denied(op.Principal, op.Path)
		}
		return rangeNotSatisfiable(op.Range.Selector)
	}
	denied := func() error { return Denied(op.Principal, op.Path) }
	served := func(n *html.Node) string {
		if view != nil {
			return view.served(n)
		}
		return dom.OuterHTML(n)
	}
	rep := Negotiate(op.Accept, true)
	if rep == RepHTML {
		m := sel.MatchFirst(comp.Root)
		if m == nil {
			return nil, absent()
		}
		if !AuthorizeRead(snap, op, m) {
			return nil, denied()
		}
		frag := served(m)
		h.Set("Content-Type", markupType(doc.ContentType))
		h.Set("Content-Range", "selector "+op.Range.Selector)
		h.Set("ETag", FragmentETag(frag, doc.Version)) // the element tag writes compare If-Match with
		if comp.Fragment != nil {
			if src, ok := comp.Fragment(m); ok {
				frag = src // the element's bytes as served in the composed page
			}
		}
		return &ReadResult{Status: http.StatusPartialContent, Header: h, Body: []byte(frag)}, nil
	}
	// All matches: every one must be readable (R-RW-128; kept although live
	// PageLove serves them, decisions.md rw.authz.denied-fragment-read).
	matches := sel.MatchAll(comp.Root)
	if len(matches) == 0 {
		return nil, absent()
	}
	for _, m := range matches {
		if !AuthorizeRead(snap, op, m) {
			return nil, denied()
		}
	}
	h.Set("Vary", AllMatchVary)
	if rep == RepJSONLD {
		js, err := jsonLD(matches)
		if err != nil {
			return nil, err
		}
		h.Set("Content-Type", "application/ld+json; charset=utf-8")
		h.Set("Content-Range", "selector "+op.Range.Selector)
		h.Set("ETag", WeakETagOf(js))
		return &ReadResult{Status: http.StatusPartialContent, Header: h, Body: js}, nil
	}
	parts := make([]Part, len(matches))
	for i, m := range matches {
		parts[i] = MatchPart(doc, op.Range.Selector, served(m))
	}
	body, ctype := MultipartBody(parts)
	h.Set("Content-Type", ctype)
	return &ReadResult{Status: http.StatusOK, Header: h, Body: body}, nil
}

// MatchPart is one part of an all-matches answer on the public plane (GET
// with Accept: multipart/mixed, css QUERY), as PageLove forms it (live
// 2026-09-28, superseding R-RW-31): the document as an attachment filename,
// the request's own selector as Content-Range, the fragment's tag and
// length.
func MatchPart(doc *store.Document, selText, frag string) Part {
	tag := FragmentETag(frag, doc.Version)
	return Part{Body: frag, ETag: tag, Header: [][2]string{
		{"Content-Disposition", `attachment; filename="` + doc.Path + `"`},
		{"Content-Type", markupType(doc.ContentType)},
		{"Content-Range", "selector " + selText},
		{"ETag", tag},
		{"Content-Length", strconv.Itoa(len(frag))},
	}}
}

// QueryParts evaluates an authoring-plane css-selector QUERY against one
// document's raw stored markup (R-PROTO-52) and returns one part per match,
// in document order, as PageLove forms them (live 2026-09-28): media type,
// the source document, a document-rooted nth-child selector for the match,
// its fragment tag, the document's modification time and the length. (The
// public plane answers a css QUERY exactly like a GET, ReadMarkup.)
func (e *Engine) QueryParts(ctx context.Context, s *site.Site, snap *site.Snapshot, doc *store.Document, op *ReadOp, compose Composer, sel *selector.Selector) ([]Part, error) {
	root, err := site.ParseMarkup(doc.ContentType, doc.Body)
	if err != nil {
		return nil, nil // malformed XML: nothing to match
	}
	view := newDocView(root, doc)
	if compose != nil && op.Plane == Public {
		comp, err := compose(ctx, s, snap, doc, root, op)
		if err != nil {
			return nil, err
		}
		if comp.Changed || comp.Root != root {
			root, view = comp.Root, nil
		}
	}
	matches := sel.MatchAll(root)
	parts := make([]Part, 0, len(matches))
	mod := time.UnixMilli(doc.ModifiedMS).UTC().Format(http.TimeFormat)
	// PageLove's tree has no element the source did not write: an implied,
	// empty <head> does not count among <body>'s preceding siblings.
	var implied dom.Implied
	if view != nil && !view.xml {
		implied = dom.ImpliedFor(root, string(doc.Body))
	}
	skip := func(n *html.Node) bool { return implied[n] && n.Data == "head" && n.FirstChild == nil }
	for _, m := range matches {
		if !AuthorizeRead(snap, op, m) {
			return nil, Denied(op.Principal, op.Path)
		}
		frag := dom.OuterHTML(m)
		if view != nil {
			frag = view.served(m)
		}
		tag := FragmentETag(frag, doc.Version)
		parts = append(parts, Part{Body: frag, ETag: tag, Header: [][2]string{
			{"Content-Type", markupType(doc.ContentType)},
			{"Content-Location", (&url.URL{Path: doc.Path}).EscapedPath()},
			{"Content-Range", "selector " + NthChildPath(m, skip)},
			{"ETag", tag},
			{"Last-Modified", mod},
			{"Content-Length", strconv.Itoa(len(frag))},
		}})
	}
	return parts, nil
}

// NthChildPath is the document-rooted selector PageLove reports for a
// QUERY match: every step "name:nth-child(i)", html and body included
// (live 2026-09-28). Siblings for which skip reports true are not counted.
func NthChildPath(n *html.Node, skip func(*html.Node) bool) string {
	var steps []string
	for x := n; x != nil && x.Type == html.ElementNode; x = x.Parent {
		i := 1
		for c := x.PrevSibling; c != nil; c = c.PrevSibling {
			if c.Type == html.ElementNode && (skip == nil || !skip(c)) {
				i++
			}
		}
		steps = append(steps, selector.EscapeIdent(x.Data)+":nth-child("+strconv.Itoa(i)+")")
	}
	for i, j := 0, len(steps)-1; i < j; i, j = i+1, j-1 {
		steps[i], steps[j] = steps[j], steps[i]
	}
	return strings.Join(steps, " > ")
}

// Part is one part of a multipart/mixed answer: its headers, in order, and
// its body.
type Part struct {
	Header [][2]string
	Body   string
	ETag   string // the part's fragment tag (also among Header)
}

// MultipartETag is the tag of an authoring QUERY answer: a function of the
// selector text and every part's tag, so it changes exactly when the
// selector, the match set or a matched fragment changes (R-PROTO-56). Its
// shape is PageLove's: one quoted hash.
func MultipartETag(selectorText string, parts []Part) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n", selectorText)
	for _, p := range parts {
		fmt.Fprintf(h, "%s\n", p.ETag)
	}
	return `"` + hex.EncodeToString(h.Sum(nil)) + `"`
}

// MultipartBody serializes parts as multipart/mixed in PageLove's framing
// (R-PROTO-4, live 2026-09-28): CRLF line breaks, "Name: value" headers, an
// unquoted alphanumeric boundary as the last Content-Type parameter, no
// preamble; the close delimiter ends the body with no line break. Parts
// without a body (OPTIONS) end at their header block's blank line and the
// body then ends with a line break.
func MultipartBody(parts []Part) ([]byte, string) {
	boundary := NewBoundary()
	var b bytes.Buffer
	headersOnly := len(parts) > 0
	for _, p := range parts {
		fmt.Fprintf(&b, "--%s\r\n", boundary)
		for _, kv := range p.Header {
			fmt.Fprintf(&b, "%s: %s\r\n", kv[0], kv[1])
		}
		if p.Body == "" {
			b.WriteString("\r\n")
			continue
		}
		headersOnly = false
		fmt.Fprintf(&b, "\r\n%s\r\n", p.Body)
	}
	fmt.Fprintf(&b, "--%s--", boundary)
	if headersOnly {
		b.WriteString("\r\n")
	}
	return b.Bytes(), "multipart/mixed; boundary=" + boundary
}

const boundaryAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// NewBoundary returns a random multipart boundary shaped like PageLove's:
// "boundary" and 32 alphanumerics.
func NewBoundary() string {
	var r [32]byte
	rand.Read(r[:])
	out := make([]byte, len(r))
	for i, c := range r {
		out[i] = boundaryAlphabet[int(c)%len(boundaryAlphabet)]
	}
	return "boundary" + string(out)
}

func contentTypeFor(ct string) string {
	if ct == "" {
		return "text/html"
	}
	return ct
}
