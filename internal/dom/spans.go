package dom

import (
	"errors"
	"sort"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/sky-valley/pagelike/internal/xmldom"
)

// Source-preserving writes (decision 0003 §6, live observation LO-1):
// PageLove serves the bytes it stored, so a selector write must change only
// the bytes of the element it addresses. The engine locates elements in the
// stored source (ComputeSpans for HTML, ParseXMLWithSpans for XML), splices
// the new text in (Splice), re-parses, and keeps the result only when the
// DOM equals the DOM-mutation result.

// Span locates an element in the source text it was parsed from.
//
//	src[Start:InnerStart]    the start tag
//	src[InnerStart:InnerEnd] the content
//	src[InnerEnd:End]        the end tag ("" when the end tag was omitted)
type Span struct {
	Start, InnerStart, InnerEnd, End int
	ExplicitEnd                      bool
	// NoContent marks elements with no content region to insert into: void
	// elements (<br>), and self-closed foreign, prefixed or XML elements.
	NoContent bool
}

// Spans correlates the elements of a parsed document with its source.
// Clean reports whether every start tag and every non-implied element were
// matched in order; when it is false (e.g. foster parenting or
// adoption-agency reordering), callers must not splice and should
// re-serialize instead.
type Spans struct {
	By    map[*html.Node]Span
	Clean bool
}

type tok struct {
	tt         html.TokenType
	name       string
	start, end int
}

func tokens(src string) ([]tok, bool) {
	var out []tok
	z := html.NewTokenizer(strings.NewReader(src))
	off := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		raw := len(z.Raw())
		t := tok{tt: tt, start: off, end: off + raw}
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken || tt == html.EndTagToken {
			n, _ := z.TagName()
			t.name = string(n)
		}
		out = append(out, t)
		off += raw
	}
	return out, off == len(src)
}

var implicitAllowed = map[atom.Atom]bool{atom.Html: true, atom.Head: true, atom.Body: true, atom.Tbody: true, atom.Colgroup: true}

// ComputeSpans builds Spans for doc, an HTML tree parsed from src (a
// document, or a holder whose children are a parsed fragment). src is
// parsed again with BuildTree, which records exact spans, and the two trees
// are walked together; Clean is false when they differ (doc was changed
// after parsing), and then only the elements before the first difference
// have spans.
func ComputeSpans(doc *html.Node, src string) Spans {
	fresh, sp := BuildTree(src)
	res := Spans{By: map[*html.Node]Span{}}
	var walk func(a, b *html.Node) bool
	walk = func(a, b *html.Node) bool {
		if a.Type != b.Type || a.Data != b.Data {
			return false
		}
		if a.Type == html.ElementNode {
			res.By[a] = sp.By[b]
		}
		ca, cb := a.FirstChild, b.FirstChild
		for ; ca != nil && cb != nil; ca, cb = ca.NextSibling, cb.NextSibling {
			if !walk(ca, cb) {
				return false
			}
		}
		return ca == nil && cb == nil
	}
	res.Clean = walk(doc, fresh)
	return res
}

func isAncestor(a, n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p == a {
			return true
		}
	}
	return false
}

// ParseXMLWithSpans parses an XML-family document and returns the source
// span of every element (see xmldom.ParseWithSpans).
func ParseXMLWithSpans(src []byte) (*html.Node, Spans, error) {
	doc, xs, err := xmldom.ParseWithSpans(src, xmldom.Options{})
	if err != nil {
		return nil, Spans{}, err
	}
	sp := Spans{By: make(map[*html.Node]Span, len(xs.By)), Clean: xs.Clean}
	for n, s := range xs.By {
		sp.By[n] = Span{Start: s.Start, InnerStart: s.InnerStart, InnerEnd: s.InnerEnd, End: s.End,
			ExplicitEnd: true, NoContent: s.SelfClosing}
	}
	return doc, sp, nil
}

// Placement says where an edit goes relative to its element: the four
// Range placements, plus PlaceReplace for PUT (and, with empty text,
// DELETE).
type Placement string

const (
	PlaceReplace Placement = "replace"
	PlaceAppend  Placement = "append"
	PlacePrepend Placement = "prepend"
	PlaceBefore  Placement = "before"
	PlaceAfter   Placement = "after"
)

// Edit is one change to stored source, anchored to an element of the tree
// the Spans were computed for.
type Edit struct {
	Node      *html.Node
	Placement Placement // PlaceReplace writes Text in place of the element
	Text      string
}

// ErrNoSpan means an element cannot be edited in place.
var ErrNoSpan = errors.New("element has no reliable source span")

// Splice applies edits to src by byte splicing, leaving every other byte of
// the stored document untouched. Edits must not overlap (an insertion at the
// boundary of a replaced range is fine). It returns ErrNoSpan when the
// correlation is not clean or an edit's element has no usable span.
func Splice(src []byte, sp Spans, edits ...Edit) ([]byte, error) {
	if !sp.Clean {
		return nil, ErrNoSpan
	}
	type cut struct {
		start, end int
		text       string
	}
	cuts := make([]cut, 0, len(edits))
	for _, e := range edits {
		s, ok := sp.By[e.Node]
		if !ok {
			return nil, ErrNoSpan
		}
		var c cut
		switch e.Placement {
		case PlaceReplace:
			c = cut{s.Start, s.End, e.Text}
		case PlaceBefore:
			c = cut{s.Start, s.Start, e.Text}
		case PlaceAfter:
			c = cut{s.End, s.End, e.Text}
		case PlacePrepend, PlaceAppend:
			if s.NoContent {
				return nil, ErrNoSpan
			}
			at := s.InnerStart
			if e.Placement == PlaceAppend {
				at = s.InnerEnd
			}
			c = cut{at, at, e.Text}
		default:
			return nil, errors.New("dom: unknown placement " + string(e.Placement))
		}
		if c.start < 0 || c.end > len(src) || c.start > c.end {
			return nil, ErrNoSpan
		}
		cuts = append(cuts, c)
	}
	// Ascending by position; at one position insertions come before the
	// range that starts there.
	sort.SliceStable(cuts, func(i, j int) bool {
		if cuts[i].start != cuts[j].start {
			return cuts[i].start < cuts[j].start
		}
		return cuts[i].end-cuts[i].start < cuts[j].end-cuts[j].start
	})
	out := make([]byte, 0, len(src)+64)
	pos := 0
	for _, c := range cuts {
		if c.start < pos {
			return nil, errors.New("dom: overlapping edits")
		}
		out = append(out, src[pos:c.start]...)
		out = append(out, c.text...)
		pos = c.end
	}
	return append(out, src[pos:]...), nil
}
