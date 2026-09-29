package authz

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/microdata"
	"github.com/sky-valley/pagelike/internal/xmldom"
)

// Rule and Group discovery (spec R-PERM-1..7, R-PERM-58).

// ExtractRules pulls AuthorizationRule and Group items from one stored
// document. Items inside <template> contents are inert and ignored. Only
// HTML documents declare rules: PageLove does not discover rules in XML
// documents (live 2026-09-28, superseding R-PERM-1's XML example).
func ExtractRules(path string, doc *html.Node) (rules []Rule, groups []Group) {
	if doc == nil || xmldom.IsXML(doc) {
		return nil, nil
	}
	for _, it := range microdata.OfType(doc, RuleType) {
		if inTemplate(it.Node) {
			continue
		}
		rules = append(rules, RulesFromItem(path, it)...)
	}
	for _, it := range microdata.OfType(doc, GroupType) {
		if inTemplate(it.Node) {
			continue
		}
		if g, ok := GroupFromItem(path, it); ok {
			groups = append(groups, g)
		}
	}
	return
}

// RulesFromItem converts one AuthorizationRule item into rules. It is
// exported for the schema package, which also treats items of schema
// subtypes of AuthorizationRule as rules (R-PERM-7).
//
// An item on a <table> (or a row group) is the documented "table form": each
// row holding properties is a separate rule built from that row only
// (R-PERM-2a). Malformed rules are dropped (R-PERM-3), and MOVE rules that
// carry a selector are normalized here (R-PERM-5).
func RulesFromItem(path string, it *microdata.Item) []Rule {
	var out []Rule
	for _, props := range ruleRows(it) {
		if r, ok := buildRule(path, props); ok {
			out = append(out, r)
		}
	}
	return out
}

// GroupFromItem converts a Group item (or an item of a Group subtype) into a
// Group. Items with a blank name are ignored (R-PERM-58).
func GroupFromItem(path string, it *microdata.Item) (Group, bool) {
	g := Group{Source: path}
	for _, v := range it.All("name") {
		if v = trimASCII(v); v != "" {
			g.Name = v
			break
		}
	}
	if g.Name == "" {
		return Group{}, false
	}
	g.Members = nonEmpty(it.All("member"))
	return g, true
}

func buildRule(path string, props []microdata.Prop) (Rule, bool) {
	r := Rule{Source: path}
	var actions, selectors []string
	for _, p := range props {
		if p.Item != nil {
			continue // nested items carry no string value
		}
		v := trimASCII(p.Value)
		if v == "" {
			continue
		}
		switch p.Name {
		case "actor":
			r.Actors = append(r.Actors, v)
		case "resource":
			r.Resources = append(r.Resources, v)
		case "method":
			r.Methods = append(r.Methods, strings.ToUpper(v))
		case "selector":
			selectors = append(selectors, v)
		case "action":
			a := strings.ToLower(v)
			if a == "allow" && v != p.Value {
				// PageLove compares the action untrimmed, so a padded
				// "allow" grants nothing there (live 2026-09-29); a padded
				// "deny" still refuses here (fail closed, decisions.md
				// authz.discovery.whitespace-trimmed).
				a = "allow (padded)"
			}
			actions = append(actions, a)
		}
	}
	if len(r.Actors) == 0 || len(r.Resources) == 0 || len(r.Methods) == 0 || len(actions) == 0 {
		return Rule{}, false
	}
	// Several actions: any deny wins (fail closed); otherwise every value
	// must be "allow". Anything unrecognized disables the rule.
	r.Effect = Allow
	for _, a := range actions {
		switch a {
		case "deny":
			r.Effect = Deny
		case "allow":
		default:
			if r.Effect != Deny {
				r.Effect = 0
			}
		}
	}
	if r.Effect == 0 {
		return Rule{}, false
	}
	r.Selector = strings.Join(selectors, ", ")
	// A MOVE rule must not carry a selector: an Allow is discarded in full,
	// a Deny is widened to the whole resource for all of its methods.
	if r.Selector != "" && hasMethod(r.Methods, "MOVE") {
		if r.Effect == Allow {
			return Rule{}, false
		}
		r.Selector = ""
	}
	for _, f := range [][]string{r.Actors, r.Resources, {r.Selector}} {
		for _, v := range f {
			if strings.Contains(v, "{{") {
				r.Warnings = append(r.Warnings, "Liquid {{ … }} in a rule field is literal text; use ${…} lookups")
				break
			}
		}
	}
	r.compile()
	return r, true
}

// ruleRows returns the property lists that form rules: one per item, or
// one per <tr> for the table form.
func ruleRows(it *microdata.Item) [][]microdata.Prop {
	switch it.Node.Data {
	case "table", "thead", "tbody", "tfoot":
	default:
		return [][]microdata.Prop{it.Props}
	}
	var order []*html.Node
	rows := map[*html.Node][]microdata.Prop{}
	for _, p := range it.Props {
		tr := nearestRow(it.Node, p.Node)
		if tr == nil {
			continue // outside any row: ignored
		}
		if _, seen := rows[tr]; !seen {
			order = append(order, tr)
		}
		rows[tr] = append(rows[tr], p)
	}
	out := make([][]microdata.Prop, 0, len(order))
	for _, tr := range order {
		out = append(out, rows[tr])
	}
	return out
}

// nearestRow returns the closest <tr> ancestor-or-self of n below root.
func nearestRow(root, n *html.Node) *html.Node {
	for c := n; c != nil && c != root; c = c.Parent {
		if c.Type == html.ElementNode && c.Data == "tr" {
			return c
		}
	}
	return nil
}

func inTemplate(n *html.Node) bool {
	for c := n.Parent; c != nil; c = c.Parent {
		if c.Type == html.ElementNode && c.Data == "template" && c.Namespace == "" {
			return true
		}
	}
	return false
}

// trimASCII trims ASCII whitespace (HTML's definition).
func trimASCII(s string) string { return strings.Trim(s, " \t\n\f\r") }

func nonEmpty(xs []string) []string {
	var out []string
	for _, x := range xs {
		if x = trimASCII(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func hasMethod(ms []string, m string) bool {
	for _, x := range ms {
		if strings.EqualFold(x, m) {
			return true
		}
	}
	return false
}
