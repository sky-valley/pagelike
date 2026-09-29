package schema

import (
	"context"
	"log/slog"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/microdata"
	"github.com/sky-valley/pagelike/internal/selector"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

// policyHook extends a site's authorization policy with what the schema
// registry knows (authz.Hook; R-PERM-7, R-PERM-8, R-PERM-60):
//
//   - rule selectors get the inheritance map for :isa();
//   - items of schema subtypes of https://pagelove.org/Group are groups;
//     when the most-derived schema declares includes(), membership is that
//     method called with self = the group item and the verified email
//     (fail closed on any error);
//   - items of schema subtypes of https://pagelove.org/AuthorizationRule are
//     rules with the same fields;
//   - an @read resolver on AuthorizationRule.actor (or a subtype's) expands
//     the actor values the rules are extracted with.
func policyHook(host any, p *authz.Policy) {
	snap, ok := host.(*site.Snapshot)
	if !ok || snap == nil {
		return
	}
	reg := For(snap)
	if len(reg.order) == 0 {
		return
	}
	p.SelectorOptions = &selector.ExtOptions{IsA: reg.IsA}
	for _, s := range reg.order {
		if s.URL == URLGroup || !reg.IsA(s.URL, URLGroup) {
			continue
		}
		for _, ti := range declarations(snap, s.URL) {
			g, ok := authz.GroupFromItem(ti.Path, ti.Item)
			if !ok {
				continue
			}
			if m := reg.includesMethod(s.URL); m != nil {
				g.Includes = reg.includesFunc(snap, s.URL, m, ti.Path, ti.Item.Node)
			}
			p.Groups = append(p.Groups, g)
		}
	}
	reg.ruleHook(snap, p)
}

// includesMethod returns the most-derived includes() of a group type.
func (r *Registry) includesMethod(t string) *MethodDecl {
	for _, s := range r.Chain(t) {
		for _, m := range s.Methods {
			if m.Name == "includes" && !m.Static {
				return m
			}
		}
	}
	return nil
}

func (r *Registry) includesFunc(snap *site.Snapshot, t string, m *MethodDecl, path string, node *html.Node) func(string) bool {
	return func(email string) (member bool) {
		defer func() {
			if recover() != nil {
				member = false
			}
		}()
		pd := snap.Docs[path]
		if pd == nil {
			return false
		}
		self := sessel.Queried(node, &sessel.Document{Path: path, Type: pd.Type, Root: pd.Root})
		if c := r.Class(t); c != nil {
			self.Class = c
		}
		ctx := context.Background()
		switch m.Implementation.Lang {
		case LangSessel:
			if m.Implementation.compile() != nil {
				return false
			}
			vars := map[string]sessel.Value{"email": email}
			if len(m.Params) > 0 {
				vars[m.Params[0]] = email
			}
			v, err := m.Implementation.compiled.Eval(ctx, &sessel.Env{Host: newHost(nil, snap), Self: self, HasSelf: true,
				Vars: vars, Budget: sessel.NewBudget()})
			return err == nil && sessel.Truthy(v)
		case LangJS:
			res, err := runJS(ctx, &JSCall{Slot: JSSlotMethod, Source: m.Implementation.Source, This: sessel.ToGo(self), HasThis: true,
				Args: []any{email}, Classes: r, Receiver: self, Host: newHost(nil, snap), Budget: sessel.NewBudget()})
			return err == nil && jsTruthy(res.Value)
		}
		return false
	}
}

// ruleHook adds rule-subtype items and applies @read resolvers on
// AuthorizationRule.actor.
func (r *Registry) ruleHook(snap *site.Snapshot, p *authz.Policy) {
	var ruleTypes []string
	actorRead := false
	for _, s := range r.order {
		if !r.IsA(s.URL, URLAuthorizationRule) {
			continue
		}
		ruleTypes = append(ruleTypes, s.URL)
		if s.eff != nil {
			if ep := s.eff.props["actor"]; ep != nil && len(ep.ReadChain) > 0 {
				actorRead = true
			}
		}
	}
	if len(ruleTypes) == 0 {
		return
	}
	isRuleType := map[string]bool{URLAuthorizationRule: true}
	for _, t := range ruleTypes {
		isRuleType[t] = true
	}
	// Documents whose rules must be (re)extracted.
	docs := map[string]bool{}
	for _, t := range ruleTypes {
		for _, ti := range declarations(snap, t) {
			if t != URLAuthorizationRule || actorRead {
				docs[ti.Path] = true
			}
		}
	}
	if actorRead {
		for _, ti := range declarations(snap, URLAuthorizationRule) {
			docs[ti.Path] = true
		}
	}
	if len(docs) == 0 {
		return
	}
	if actorRead {
		kept := p.Rules[:0]
		for _, rule := range p.Rules {
			if !docs[rule.Source] {
				kept = append(kept, rule)
			}
		}
		p.Rules = kept
	}
	for _, path := range snap.Paths {
		if !docs[path] {
			continue
		}
		pd := snap.Docs[path]
		if pd == nil {
			continue
		}
		root := pd.Root
		if actorRead {
			root = dom.Clone(pd.Root)
			if _, err := ApplyRead(context.Background(), nil, snap, path, root); err != nil {
				slog.Warn("schema: @read on AuthorizationRule.actor failed; rules of the document are ignored", "path", path, "err", err)
				continue
			}
		}
		for _, n := range instancesUnder(root) {
			t := itemType(n)
			if !isRuleType[t] || inTemplate(n) {
				continue
			}
			if t == URLAuthorizationRule && !actorRead {
				continue // already extracted by the site index
			}
			p.Rules = append(p.Rules, authz.RulesFromItem(path, microdata.Parse(n))...)
		}
	}
}
