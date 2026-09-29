package engine

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/sse"
	"github.com/sky-valley/pagelike/internal/store"
)

// Authoring-plane operations on collections (WebDAV DELETE, MOVE and COPY
// of directories as well as files). They bypass authorization rules and
// transition constraints like every authoring write, and announce each
// document they create, replace or remove as a whole-document mutation
// event, so live pages follow author edits (docs/spec/sse.md R-SSE-22).

// DeleteCollection removes every document under dir (which ends in "/"),
// their authored baselines and the directory markers, in one transaction.
// It returns store.ErrNotFound when the collection does not exist.
func (e *Engine) DeleteCollection(ctx context.Context, s *site.Site, dir, host string) error {
	s.WriteMu.Lock()
	defer s.WriteMu.Unlock()
	if ok, err := s.Store.DirExists(ctx, dir); err != nil {
		return err
	} else if !ok {
		return store.ErrNotFound
	}
	snap, err := s.Index(ctx)
	if err != nil {
		return err
	}
	events, err := s.Store.Update(ctx, func(tx *store.Tx) error {
		w := &WriteCtx{Site: s, Snap: snap, Tx: tx, Engine: e, Op: &Op{Plane: Authoring, Method: "DELETE", Path: dir, Host: host}}
		docs, err := tx.List(dir, false)
		if err != nil {
			return err
		}
		for _, d := range docs {
			if err := tx.Delete(d.Path); err != nil {
				return err
			}
			if err := e.event(w, d.Path, sse.Mutation{Method: "DELETE"}); err != nil {
				return err
			}
		}
		if err := tx.DeleteAuthored(dir); err != nil {
			return err
		}
		return tx.RemoveDirs(dir)
	})
	if err != nil {
		return err
	}
	publish(s, nil, events)
	return nil
}

// AuthoringCopy copies a file or collection from src to dst (both paths;
// collections end in "/") and, with move, removes the source. Overwrite
// false refuses to replace an existing destination (412). It reports
// whether anything already existed at the destination (204 rather than
// 201). A missing source is store.ErrNotFound.
func (e *Engine) AuthoringCopy(ctx context.Context, s *site.Site, src, dst string, move, overwrite bool, host string) (existed bool, err error) {
	s.WriteMu.Lock()
	defer s.WriteMu.Unlock()
	snap, err := s.Index(ctx)
	if err != nil {
		return false, err
	}
	collection := strings.HasSuffix(src, "/")
	if collection && !strings.HasSuffix(dst, "/") {
		dst += "/"
	}
	if src == dst || (collection && strings.HasPrefix(dst, src)) {
		return false, errdoc.New(http.StatusForbidden, "SameDestination", "the Destination %s is the source or inside it", dst)
	}
	events, err := s.Store.Update(ctx, func(tx *store.Tx) error {
		w := &WriteCtx{Site: s, Snap: snap, Tx: tx, Engine: e, Op: &Op{Plane: Authoring, Method: "MOVE", Path: src, Host: host}}
		var docs []*store.Document
		if collection {
			all, err := tx.List(src, true)
			if err != nil {
				return err
			}
			docs = all
		} else {
			d, err := tx.Get(src)
			if err != nil {
				return err
			}
			docs = []*store.Document{d}
		}
		if len(docs) == 0 {
			return store.ErrNotFound
		}
		for _, d := range docs {
			np := dst
			if collection {
				np = dst + strings.TrimPrefix(d.Path, src)
			}
			prev, err := tx.Get(np)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			if prev != nil {
				existed = true
				if !overwrite {
					return errdoc.New(http.StatusPreconditionFailed, "DestinationExists", "%s exists and Overwrite is F", np)
				}
			}
			c := *d
			c.Path = np
			stored, err := tx.Put(&c)
			if err != nil {
				return err
			}
			if err := tx.PutAuthored(stored); err != nil {
				return err
			}
			if err := e.event(w, np, sse.Mutation{Method: "PUT", ETag: stored.ETag}); err != nil {
				return err
			}
			if move {
				if err := tx.Delete(d.Path); err != nil {
					return err
				}
				if err := tx.DeleteAuthored(d.Path); err != nil {
					return err
				}
				if err := e.event(w, d.Path, sse.Mutation{Method: "DELETE"}); err != nil {
					return err
				}
			}
		}
		if move && collection {
			return tx.RemoveDirs(src)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	publish(s, nil, events)
	return existed, nil
}
