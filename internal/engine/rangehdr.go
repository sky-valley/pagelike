package engine

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/selector"
	"github.com/sky-valley/pagelike/internal/site"
)

// Range is a parsed Range (or Destination-Range) header.
type Range struct {
	// Unit is the lower-cased range unit ("selector", "bytes", "entries",
	// or any other token), "" when the header is absent or has no unit.
	Unit      string
	Selector  string // selector text, trimmed, without the placement sub-field
	Placement string // lower-cased placement sub-field, "" when absent
	// Params holds the sub-fields of a selector range; placement is the
	// only one defined.
	Params map[string]string
	Start  int    // entries: first index
	End    int    // entries: last index (inclusive), -1 when open
	Raw    string // the header value, trimmed

	// expanded is Selector with its selector functions replaced by their
	// values over the site (ExpandRange); it is what CheckSelector
	// compiles, while Selector stays the text echoed in Content-Range.
	expanded  string
	expandErr error
	// isa is the :isa() inheritance relation of the snapshot the range is
	// read against (BindSnapshot); nil makes :isa() match nothing.
	isa func(itemtype, target string) bool
}

// SelectorIsA, when set, gives the :isa() inheritance relation of a site
// snapshot (docs/spec/composing.md R-COMP-177): the schema package installs
// its registry's IsA. Range selectors compile with it once bound to a
// snapshot (BindSnapshot, ExpandRange, and the engine's read and write
// paths).
var SelectorIsA func(snap *site.Snapshot) func(itemtype, target string) bool

// BindSnapshot returns r with :isa() resolved against snap's schema
// hierarchy (SelectorIsA).
func (r Range) BindSnapshot(snap *site.Snapshot) Range {
	if SelectorIsA != nil && snap != nil && r.HasSelector() {
		r.isa = SelectorIsA(snap)
	}
	return r
}

// HasSelector reports whether the header addressed a selector.
func (r Range) HasSelector() bool { return r.Unit == "selector" }

// Present reports whether a Range header was sent at all.
func (r Range) Present() bool { return r.Raw != "" }

// placementRE finds the placement sub-field at the end of a selector range
// (docs/spec/reading-writing.md R-RW-10, protocol.md R-PROTO-2): the last
// "; placement=<word>" wins, so a ';' inside the selector (e.g. in a quoted
// attribute value) is preserved.
var placementRE = regexp.MustCompile(`(?is)^(.*?)\s*;\s*placement\s*=\s*([A-Za-z]+)\s*$`)

// ParseRange parses "selector=<css>[; placement=<p>]" and "entries=a-b",
// and passes other units through (callers ignore units they do not
// implement, as documented for reads).
func ParseRange(h string) Range {
	h = strings.TrimSpace(h)
	r := Range{Raw: h, End: -1}
	if h == "" {
		return r
	}
	eq := strings.IndexByte(h, '=')
	if eq < 0 {
		return r
	}
	r.Unit = strings.ToLower(strings.TrimSpace(h[:eq]))
	rest := h[eq+1:]
	switch r.Unit {
	case "selector":
		if m := placementRE.FindStringSubmatch(rest); m != nil {
			rest, r.Placement = m[1], strings.ToLower(m[2])
		}
		r.Selector = strings.TrimSpace(rest)
		r.Params = map[string]string{}
		if r.Placement != "" {
			r.Params["placement"] = r.Placement
		}
	case "entries":
		a, b, _ := strings.Cut(strings.TrimSpace(rest), "-")
		r.Start, _ = strconv.Atoi(strings.TrimSpace(a))
		if n, err := strconv.Atoi(strings.TrimSpace(b)); err == nil {
			r.End = n
		}
	}
	return r
}

// ValidPlacement reports whether p is one of the four placements.
func ValidPlacement(p string) bool {
	switch p {
	case "append", "prepend", "before", "after":
		return true
	}
	return false
}

// ExpansionFailed reports whether a selector function could not be
// evaluated (as opposed to the selector text not parsing).
func (r Range) ExpansionFailed() bool { return r.expandErr != nil }

// CheckSelector validates the selector of a selector range: an empty
// selector is a malformed range (400), one that does not parse is 422
// (docs/spec/reading-writing.md R-RW-17, compat decision C-10).
func (r Range) CheckSelector() (*selector.Selector, error) {
	if r.Selector == "" {
		return nil, errdoc.New(http.StatusBadRequest, "InvalidRange", "the selector range is empty")
	}
	if r.expandErr != nil {
		return nil, errdoc.New(http.StatusUnprocessableEntity, "InvalidSelector", "invalid selector %q: %v", r.Selector, r.expandErr)
	}
	src := r.Selector
	if r.expanded != "" {
		src = r.expanded
	}
	var sel *selector.Selector
	var err error
	if r.isa != nil {
		sel, err = selector.CompileWith(src, selector.ExtOptions{IsA: r.isa})
	} else {
		sel, err = selector.Compile(src)
	}
	if err != nil {
		return nil, errdoc.New(http.StatusUnprocessableEntity, "InvalidSelector", "invalid selector %q: %v", r.Selector, err)
	}
	return sel, nil
}
