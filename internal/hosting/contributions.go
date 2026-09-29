package hosting

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/selector"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/sse"
	"github.com/sky-valley/pagelike/internal/store"
	"golang.org/x/net/html"
)

const contributionSchema = `CREATE TABLE IF NOT EXISTS hosted_contributions(id TEXT PRIMARY KEY,owner TEXT NOT NULL,path TEXT NOT NULL,selector TEXT NOT NULL,removed INTEGER NOT NULL DEFAULT 0)`

var contributionID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type contribution struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Selector string `json:"selector"`
	Owner    string `json:"owner"`
}

// Record ownership inside the mutation transaction. The reserved marker is
// generated after input validation, so a participant cannot claim another node.
func recordContribution(_ context.Context, w *engine.WriteCtx) error {
	if w.Op.Method != "POST" && w.Op.Method != "PUT" {
		return nil
	}
	if w.Op.Principal == nil || !w.Op.Principal.Authenticated {
		return errdoc.New(401, "SignIn", "Sign in to contribute.")
	}
	save := func(n *html.Node) error {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return err
		}
		id := hex.EncodeToString(b[:])
		sel := ""
		if n != nil {
			dom.SetAttr(n, "data-contribution-id", id)
			sel = `[data-contribution-id="` + id + `"]`
		}
		_, err := w.Tx.Exec(`INSERT INTO hosted_contributions(id,owner,path,selector) VALUES (?,?,?,?)`, id, w.Op.Principal.Sub, w.Op.Path, sel)
		return err
	}
	ct := engine.ResolveContentType(w.Op.Path, w.Op.ContentType)
	if !strings.HasPrefix(ct, "text/html") {
		// Whole-resource replacement retires the old ownership in this same
		// transaction. A delayed removal/report must not touch the replacement.
		if _, err := w.Tx.Exec(`UPDATE hosted_contributions SET removed=1 WHERE path=? AND removed=0`, w.Op.Path); err != nil {
			return err
		}
		return save(nil)
	}
	root, err := dom.Parse(w.Op.Body)
	if err != nil {
		return err
	}
	for n := root.FirstChild; n != nil; n = n.NextSibling {
		if n.Type == html.TextNode && strings.TrimSpace(n.Data) != "" {
			return errdoc.New(422, "ContributionElement", "Wrap contributed text in an HTML element so its author can remove it.")
		}
		if n.Type == html.ElementNode {
			if err := save(n); err != nil {
				return err
			}
		}
	}
	w.Op.Body = dom.Render(root)
	return nil
}

func (h *Server) contributions(w http.ResponseWriter, r *http.Request, s *site.Site, owner, id string, moderator bool) {
	if r.Method == "GET" && id == "" {
		query := `SELECT id,owner,path,selector FROM hosted_contributions WHERE removed=0`
		var args []any
		if !moderator {
			query += ` AND owner=?`
			args = append(args, owner)
		}
		query += ` ORDER BY id LIMIT 1000`
		rows, err := s.Store.DB().QueryContext(r.Context(), query, args...)
		if err != nil {
			fail(w, 503)
			return
		}
		defer rows.Close()
		out := []contribution{}
		for rows.Next() {
			var c contribution
			if rows.Scan(&c.ID, &c.Owner, &c.Path, &c.Selector) != nil {
				fail(w, 503)
				return
			}
			out = append(out, c)
		}
		if rows.Err() != nil {
			fail(w, 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
		return
	}
	if r.Method != "DELETE" || !contributionID.MatchString(id) {
		fail(w, 404)
		return
	}
	err := h.removeContribution(r.Context(), s, id, owner, moderator)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404)
		return
	}
	var problem *errdoc.Error
	if errors.As(err, &problem) {
		fail(w, 409)
		return
	}
	if err != nil {
		fail(w, 503)
		return
	}
	w.WriteHeader(204)
}

func (h *Server) removeContribution(ctx context.Context, s *site.Site, id, owner string, moderator bool) error {
	s.WriteMu.Lock()
	defer s.WriteMu.Unlock()
	snap, err := s.Index(ctx)
	if err != nil {
		return err
	}
	events, err := s.Store.Update(ctx, func(tx *store.Tx) error {
		var c contribution
		var removed int
		if err := tx.QueryRow(`SELECT owner,path,selector,removed FROM hosted_contributions WHERE id=?`, id).Scan(&c.Owner, &c.Path, &c.Selector, &removed); err != nil {
			return err
		}
		if !moderator && c.Owner != owner {
			return sql.ErrNoRows
		}
		if removed != 0 {
			return nil
		}
		doc, err := tx.Get(c.Path)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err == nil {
			var targets []*html.Node
			if c.Selector == "" {
				// An upload/document replacement may have a later owner's contribution.
				var other int
				if err := tx.QueryRow(`SELECT COUNT(*) FROM hosted_contributions WHERE path=? AND id<>? AND removed=0`, c.Path, id).Scan(&other); err != nil {
					return err
				}
				if other > 0 {
					return errdoc.New(409, "ChangedContribution", "This resource has changed since your contribution.")
				}
				if err := tx.Delete(c.Path); err != nil {
					return err
				}
			} else {
				root, e := dom.Parse(doc.Body)
				if e != nil {
					return e
				}
				sel, e := selector.Compile(c.Selector)
				if e != nil {
					return e
				}
				n := sel.MatchFirst(root)
				if n != nil && n.Parent != nil {
					var nested bool
					dom.Walk(n, func(child *html.Node) bool {
						if child != n && dom.HasAttr(child, "data-contribution-id") {
							nested = true
						}
						return true
					})
					if nested && !moderator {
						return errdoc.New(409, "NestedContributions", "This contribution contains later participation. Ask the creator to moderate it.")
					}
					targets = append(targets, n)
					n.Parent.RemoveChild(n)
					doc.Body = dom.Render(root)
					if _, e = tx.Put(doc); e != nil {
						return e
					}
				}
			}
			event := store.Event{Path: c.Path, Name: "mutation", Data: (sse.Mutation{Method: "DELETE", Path: c.Path, Host: s.Name + "." + h.cfg.Domain, Selector: c.Selector}).Render(), Targets: targets}
			if engine.EventAudience != nil {
				event.Also = engine.EventAudience(snap, c.Path)
			}
			if err := tx.AddEvent(event); err != nil {
				return err
			}
		}
		_, err = tx.Exec(`UPDATE hosted_contributions SET removed=1 WHERE id=?`, id)
		return err
	})
	if err == nil {
		s.Broker.Publish(events)
	}
	return err
}

func (h *Server) adminContributions(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) < 2 || len(parts) > 3 || parts[1] != "contributions" || !site.ValidName(parts[0]) {
		fail(w, 404)
		return
	}
	s, err := h.core.Sites.Get(r.Context(), parts[0])
	if err != nil {
		// Missing site storage is not proof that a contribution was removed.
		// The operator must repair/reconcile storage before acknowledging it.
		fail(w, 503)
		return
	}
	id := ""
	if len(parts) == 3 {
		id = parts[2]
	}
	h.contributions(w, r, s, "", id, true)
}
