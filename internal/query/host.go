// Package query is the glue between the Sessel runtime and a site: a
// read-only sessel.Host over a site snapshot, and the public-plane
// `QUERY … Content-Type: text/sessel` handler (docs/spec/protocol.md §7–§8,
// docs/spec/sessel.md R-SESSEL-370). It registers itself with
// server.Extend and is linked in by internal/features.
package query

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

// ClassSource builds the schema class registry of a snapshot.
type ClassSource func(snap *site.Snapshot) sessel.ClassRegistry

var classSource atomic.Pointer[ClassSource]

// SetClassSource installs the schema package's class registry, replacing
// the default minimal reader of Schema items (sessel.MicrodataClasses).
// Implementations should cache per snapshot (site.Snapshot.Ext).
func SetClassSource(f ClassSource) { classSource.Store(&f) }

// index is the per-snapshot Sessel view of the site: its markup documents
// wrapped as sessel Documents (shared, immutable) and the default classes.
type index struct {
	all    []*sessel.Document
	byPath map[string]*sessel.Document

	classOnce sync.Once
	classes   *sessel.ClassSet
}

const extKey = "sessel.query"

func indexFor(snap *site.Snapshot) *index {
	return snap.Ext(extKey, func(s *site.Snapshot) any {
		ix := &index{byPath: make(map[string]*sessel.Document, len(s.Paths))}
		for _, p := range s.Paths {
			pd := s.Docs[p]
			if pd == nil {
				continue
			}
			d := &sessel.Document{Path: pd.Path, Type: pd.Type, Root: pd.Root}
			ix.all = append(ix.all, d)
			ix.byPath[p] = d
		}
		return ix
	}).(*index)
}

func (ix *index) defaultClasses() *sessel.ClassSet {
	ix.classOnce.Do(func() { ix.classes = sessel.MicrodataClasses(ix.all) })
	return ix.classes
}

// Host is a read-only sessel.Host over one snapshot of a site: whole-site
// selector scope in path order, from-clause paths and globs (the
// AuthorizationRule glob language), Pagelove.GET, schema classes. It has no
// write provider; hosts that allow Pagelove.PUT/DELETE (reactions) set
// WriteProvider.
type Host struct {
	Site *site.Site
	Snap *site.Snapshot
	// WriteProvider is returned by Writer (nil: writes are refused).
	WriteProvider sessel.Writer
	// Clock overrides time.Now for Temporal.Now.
	Clock func() time.Time

	ix *index
}

// NewHost returns a read-only host over snap.
func NewHost(s *site.Site, snap *site.Snapshot) *Host {
	return &Host{Site: s, Snap: snap, ix: indexFor(snap)}
}

// Document returns the markup document at path, or nil.
func (h *Host) Document(path string) *sessel.Document { return h.ix.byPath[path] }

// Documents implements sessel.Host.
func (h *Host) Documents(ctx context.Context, pattern string) ([]*sessel.Document, error) {
	if pattern == "" {
		return h.ix.all, nil
	}
	if !sessel.IsGlob(pattern) {
		if d := h.ix.byPath[pattern]; d != nil {
			return []*sessel.Document{d}, nil
		}
		return nil, nil
	}
	var out []*sessel.Document
	for _, d := range h.ix.all {
		if authz.GlobMatch(pattern, d.Path) {
			out = append(out, d)
		}
	}
	return out, nil
}

// Resource implements sessel.Host (Pagelove.GET).
func (h *Host) Resource(ctx context.Context, path string) (sessel.Value, error) {
	sd, err := h.Site.Store.Get(ctx, path)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	meta := sessel.NewDict()
	meta.Set("mimetype", sd.ContentType)
	meta.Set("etag", sd.ETag)
	meta.Set("created", time.UnixMilli(sd.CreatedMS).UTC().Format(time.RFC3339))
	meta.Set("modified", time.UnixMilli(sd.ModifiedMS).UTC().Format(time.RFC3339))
	meta.Set("size", sd.Size)
	meta.Set("version", sd.Version)
	if d := h.ix.byPath[path]; d != nil && !sd.IsBlob() {
		doc := &sessel.Document{Path: d.Path, Type: d.Type, Root: d.Root, Meta: meta}
		return doc.Element(), nil
	}
	return &sessel.Blob{Path: path, Meta: meta}, nil
}

// Class implements sessel.Host.
func (h *Host) Class(ctx context.Context, url string) (sessel.Class, error) {
	if f := classSource.Load(); f != nil {
		if reg := (*f)(h.Snap); reg != nil {
			return reg.Class(url), nil
		}
		return nil, nil
	}
	if c := h.ix.defaultClasses().Class(url); c != nil {
		return c, nil
	}
	return nil, nil
}

// Writer implements sessel.Host.
func (h *Host) Writer() sessel.Writer { return h.WriteProvider }

// Now implements sessel.Host.
func (h *Host) Now() time.Time {
	if h.Clock != nil {
		return h.Clock()
	}
	return time.Now()
}
