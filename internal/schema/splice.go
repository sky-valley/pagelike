package schema

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/site"
)

// srcDoc is a parsed markup document whose mutations are tracked as edits
// anchored to the elements of the pristine parse, so the result can be
// spliced into the source bytes and every untouched byte survives
// (decision 0003 §6, LO-1). Without a source (selector writes, whose
// storage the engine splices itself) it only mutates the tree.
type srcDoc struct {
	path  string
	ct    string
	src   []byte
	root  *html.Node
	spans *dom.Spans

	replaced []replaceEdit
	appended map[*html.Node][]*html.Node
	appOrder []*html.Node
	changed  bool
}

type replaceEdit struct {
	orig   *html.Node   // pristine element replaced
	parent *html.Node   // its parent at the time (must stay attached)
	with   []*html.Node // replacement nodes (rendered at the end)
}

func newSrcDoc(path, ct string, src []byte, root *html.Node) *srcDoc {
	return &srcDoc{path: path, ct: ct, src: src, root: root, appended: map[*html.Node][]*html.Node{}}
}

// track computes source spans before the first mutation.
func (d *srcDoc) track() {
	if d.spans == nil && d.src != nil && !site.IsXML(d.ct) {
		sp := dom.ComputeSpans(d.root, string(d.src))
		d.spans = &sp
	}
}

func (d *srcDoc) pristine(n *html.Node) bool {
	if d.spans == nil {
		return false
	}
	_, ok := d.spans.By[n]
	return ok
}

// replace puts nodes in place of orig.
func (d *srcDoc) replace(orig *html.Node, nodes []*html.Node) {
	d.track()
	d.changed = true
	parent := orig.Parent
	if parent == nil {
		return
	}
	if d.pristine(orig) {
		d.replaced = append(d.replaced, replaceEdit{orig: orig, parent: parent, with: nodes})
	} else {
		// A node inserted by an earlier edit: that edit now renders the
		// replacement in its place.
		sub := func(list []*html.Node) ([]*html.Node, bool) {
			for i, x := range list {
				if x == orig {
					out := append(append(append([]*html.Node{}, list[:i]...), nodes...), list[i+1:]...)
					return out, true
				}
			}
			return list, false
		}
		done := false
		for i := range d.replaced {
			if d.replaced[i].with, done = sub(d.replaced[i].with); done {
				break
			}
		}
		if !done {
			if l, ok := d.appended[parent]; ok {
				d.appended[parent], _ = sub(l)
			}
		}
	}
	for _, n := range nodes {
		if n.Parent != nil {
			n.Parent.RemoveChild(n)
		}
		parent.InsertBefore(n, orig)
	}
	parent.RemoveChild(orig)
}

// remove deletes n.
func (d *srcDoc) remove(n *html.Node) { d.replace(n, nil) }

// appendChild adds n as the last child of parent.
func (d *srcDoc) appendChild(parent, n *html.Node) {
	d.track()
	d.changed = true
	if d.pristine(parent) {
		if _, ok := d.appended[parent]; !ok {
			d.appOrder = append(d.appOrder, parent)
		}
		d.appended[parent] = append(d.appended[parent], n)
	}
	parent.AppendChild(n)
}

// bytes returns the new source: the storage serialization of the tree.
// PageLove stores HTML writes serialized (LO-15) and XML is re-serialized
// too, so the splicing below is no longer reached; it is kept until the
// source-editing helpers are retired.
func (d *srcDoc) bytes() []byte {
	want := dom.RenderStorage(d.root, nil)
	if want != nil || site.IsXML(d.ct) {
		return want
	}
	if d.spans != nil && d.spans.Clean {
		var edits []dom.Edit
		ok := true
		for _, r := range d.replaced {
			if !attached(d.root, r.parent) {
				continue // inside a later replacement, which renders it
			}
			edits = append(edits, dom.Edit{Node: r.orig, Placement: dom.PlaceReplace, Text: render(r.with)})
		}
		for _, p := range d.appOrder {
			if !attached(d.root, p) {
				continue
			}
			var live []*html.Node
			for _, n := range d.appended[p] {
				if n.Parent == p {
					live = append(live, n)
				}
			}
			if len(live) == 0 {
				continue
			}
			sp := d.spans.By[p]
			if sp.NoContent {
				ok = false
				break
			}
			edits = append(edits, dom.Edit{Node: p, Placement: dom.PlaceAppend, Text: render(live)})
		}
		if ok {
			if out, err := dom.Splice(d.src, *d.spans, edits...); err == nil {
				if got, err := site.ParseMarkup(d.ct, out); err == nil && bytes.Equal(dom.RenderStorage(got, nil), want) {
					return out
				}
			}
		}
	}
	return dom.RenderStorage(d.root, dom.ImpliedFor(d.root, string(d.src)))
}

func render(nodes []*html.Node) string {
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(dom.StorageHTML(n))
	}
	return b.String()
}
