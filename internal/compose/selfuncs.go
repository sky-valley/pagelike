package compose

import (
	"context"
	"fmt"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/selector"
	"github.com/sky-valley/pagelike/internal/site"
)

// siteFuncs evaluates selector functions (count(), text-of(), value-of(),
// attr-of()) over every stored document of the site (R-COMP-178). Like
// bindings, they read the whole site regardless of authorization
// (R-COMP-32; see Q-18).
type siteFuncs struct{ snap *site.Snapshot }

func (f *siteFuncs) all(src string) ([]*html.Node, error) {
	sel, err := selector.Compile(src)
	if err != nil {
		return nil, err
	}
	var out []*html.Node
	for _, p := range f.snap.Paths {
		if pd := f.snap.Docs[p]; pd != nil {
			out = append(out, sel.MatchAll(pd.Root)...)
		}
	}
	return out, nil
}

func (f *siteFuncs) one(src string) (*html.Node, error) {
	m, err := f.all(src)
	if err != nil {
		return nil, err
	}
	switch len(m) {
	case 0:
		return nil, nil
	case 1:
		return m[0], nil
	}
	return nil, fmt.Errorf("%q matches %d elements; a selector function needs at most one", src, len(m))
}

// Count implements selector.FuncEvaluator.
func (f *siteFuncs) Count(src string) (int, error) {
	m, err := f.all(src)
	return len(m), err
}

// TextOf implements selector.FuncEvaluator.
func (f *siteFuncs) TextOf(src string) (string, error) {
	n, err := f.one(src)
	if err != nil || n == nil {
		return "", err
	}
	return selector.TextContent(n), nil
}

// ValueOf implements selector.FuncEvaluator.
func (f *siteFuncs) ValueOf(src string) (string, error) {
	n, err := f.one(src)
	if err != nil || n == nil {
		return "", err
	}
	return selector.MicrodataValue(n), nil
}

// AttrOf implements selector.FuncEvaluator.
func (f *siteFuncs) AttrOf(name, src string) (string, error) {
	n, err := f.one(src)
	if err != nil || n == nil {
		return "", err
	}
	return attr(n, name), nil
}

// ExpandSelector replaces the selector functions of a Range selector with
// their values over the site (R-COMP-178). Selectors without functions
// are returned unchanged.
func ExpandSelector(ctx context.Context, s *site.Site, snap *site.Snapshot, src string) (string, error) {
	if !hasSelectorFunction(src) {
		return src, nil
	}
	return selector.ExpandFunctions(src, &siteFuncs{snap: snap})
}
