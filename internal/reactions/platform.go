package reactions

import (
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/microdata"
)

// Platform schemas of reaction items, as PageLove hosts enforce them on
// serving-path writes (R-REACT-4a, R-REACT-62). Handler is the base of
// Trigger, Processor and TransitionHandler; RequestHandler of Trigger and
// Processor.
const (
	TypeHandler        = "https://pagelove.org/Handler"
	TypeRequestHandler = "https://pagelove.org/RequestHandler"
	TypeHTTPMethod     = "https://pagelove.org/HTTPMethod"
)

// httpMethods are the values of the HTTPMethod enumeration, compared
// case-sensitively (live 2026-09-29).
var httpMethods = []string{"GET", "PUT", "POST", "DELETE", "PATCH", "MOVE", "*"}

// problem is one platform-schema violation.
type problem struct {
	itemtype, property, check, message string
}

// checkPlatformItems rejects a serving-path write that stores a reaction
// item its platform schema refuses (422 SchemaViolation):
//   - a TransitionConstraint without selector or property, or with neither
//     from nor to (R-REACT-62);
//   - a Trigger, Processor or TransitionHandler without exactly one action,
//     or with more than one when (Handler);
//   - a Trigger or Processor method outside the HTTPMethod enumeration
//     (RequestHandler).
//
// A whole-document PUT checks every item of the new document. A selector
// PUT or POST checks the items it inserts and, for handlers, the handler
// items enclosing the insertion; a selector DELETE checks the handler items
// that enclosed the removed element. Items the write does not touch are not
// checked, so a handler stored invalid over WebDAV does not block writes
// elsewhere in its document.
func checkPlatformItems(w *engine.WriteCtx, types *typeInfo) error {
	var roots, enclosing []*html.Node
	sel := w.Op.Range.HasSelector()
	switch {
	case !sel && w.Op.Method == http.MethodPut:
		if w.After != nil {
			roots = []*html.Node{w.After}
		}
	case sel && (w.Op.Method == http.MethodPut || w.Op.Method == http.MethodPost):
		roots = w.Inserted
		for _, n := range w.Inserted {
			enclosing = append(enclosing, n.Parent)
		}
	case sel && w.Op.Method == http.MethodDelete:
		if w.After != nil && w.Target != nil && w.Target.Parent != nil {
			enclosing = append(enclosing, nodeAtPath(w.After, pathOf(w.Target.Parent)))
		}
	}
	var probs []problem
	seen := map[*html.Node]bool{}
	check := func(n *html.Node, handlersOnly bool) {
		if seen[n] {
			return
		}
		seen[n] = true
		kinds := types.reactionKinds(n)
		if len(kinds) == 0 {
			return
		}
		it := microdata.Parse(n)
		if !handlersOnly && containsStr(kinds, TypeTransitionConstraint) {
			probs = append(probs, constraintProblems(it)...)
		}
		probs = append(probs, handlerProblems(it, kinds)...)
	}
	for _, root := range roots {
		walkItems(root, func(n *html.Node) { check(n, false) })
	}
	for _, p := range enclosing {
		for e := p; e != nil && e.Type == html.ElementNode; e = e.Parent {
			if isItem(e) {
				check(e, true)
			}
		}
	}
	if len(probs) == 0 {
		return nil
	}
	return &errdoc.Error{Status: http.StatusUnprocessableEntity, Kind: "SchemaViolation", Message: "Schema validation failed", Document: platformViolationDocument(probs)}
}

// constraintProblems checks a TransitionConstraint (R-REACT-62).
func constraintProblems(it *microdata.Item) []problem {
	var out []problem
	add := func(prop, msg string) {
		out = append(out, problem{TypeTransitionConstraint, prop, "schema", msg})
	}
	if len(filterValues(it, "selector")) == 0 {
		add("selector", "a transition constraint needs a selector")
	}
	if len(filterValues(it, "property")) == 0 {
		add("property", "a transition constraint needs a property")
	}
	_, hasFrom := stateValues(it, "from")
	_, hasTo := stateValues(it, "to")
	if !hasFrom && !hasTo {
		add("from", "a transition constraint declares neither from nor to")
	}
	return out
}

// handlerProblems checks the Handler and RequestHandler properties of a
// Trigger, Processor or TransitionHandler (R-REACT-4a), with PageLove's
// messages (live 2026-09-29).
func handlerProblems(it *microdata.Item, kinds []string) []problem {
	handler := containsStr(kinds, TypeTrigger) || containsStr(kinds, TypeProcessor) || containsStr(kinds, TypeTransitionHandler)
	if !handler {
		return nil
	}
	var out []problem
	actions, whens := 0, 0
	for _, p := range it.Props {
		switch p.Name {
		case "action":
			actions++
		case "when":
			whens++
		}
	}
	if actions != 1 {
		out = append(out, problem{TypeHandler, "action", "cardinality",
			"cardinality 1..1 violated: expected exactly 1 value, found " + strconv.Itoa(actions)})
	}
	if whens > 1 {
		out = append(out, problem{TypeHandler, "when", "cardinality",
			"cardinality 0..1 violated: expected at most 1 value, found " + strconv.Itoa(whens)})
	}
	if containsStr(kinds, TypeTrigger) || containsStr(kinds, TypeProcessor) {
		for _, m := range filterValues(it, "method") {
			if !containsStr(httpMethods, m) {
				out = append(out, problem{TypeRequestHandler, "method", "enum",
					`Value "` + m + `" is not a valid ` + TypeHTTPMethod + ` (expected one of: "` + strings.Join(httpMethods, `", "`) + `")`})
			}
		}
	}
	return out
}

// platformViolationDocument renders the 422 SchemaViolation body; each
// message reads "[<itemtype>].<property>: <message>" as on PageLove.
func platformViolationDocument(probs []problem) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html>\n  <head>\n    <title>422 Unprocessable Entity - Schema Violation</title>\n  </head>\n")
	b.WriteString(`  <body itemscope itemtype="` + TypeSchemaViolation + `">` + "\n")
	b.WriteString("    <h1 itemprop=\"name\">Unprocessable Entity</h1>\n    <meta itemprop=\"statusCode\" content=\"422\">\n    <p itemprop=\"description\">Schema validation failed</p>\n    <ul>\n")
	for _, p := range probs {
		b.WriteString(`      <li itemprop="violations" itemscope itemtype="` + TypeViolation + `">`)
		b.WriteString(`<span itemprop="check">` + textEsc(p.check) + `</span>`)
		b.WriteString(`<span itemprop="itemtype">` + p.itemtype + `</span>`)
		b.WriteString(`<span itemprop="property">` + textEsc(p.property) + `</span>`)
		b.WriteString(`<span itemprop="message">[` + p.itemtype + `].` + textEsc(p.property) + `: ` + textEsc(p.message) + `</span></li>` + "\n")
	}
	b.WriteString("    </ul>\n  </body>\n</html>\n")
	return b.String()
}
