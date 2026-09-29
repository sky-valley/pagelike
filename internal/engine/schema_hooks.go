package engine

import (
	"github.com/sky-valley/pagelike/internal/sse"
	"github.com/sky-valley/pagelike/internal/store"
)

// Side-effect writes for hooks that change other documents as a consequence
// of a write, without re-entering the write pipeline: schema referential
// cascades (docs/spec/modeling.md R-MOD-41/42: "every cascade effect commits
// in the same transaction and emits the usual events"). They run inside the
// write's transaction, skip authorization and hooks, keep the authored
// baseline in step on the authoring plane, record their mutation events
// attributed to no session (like trigger side effects, so the writer also
// sees them), and are listed in w.Extra.
//
// Contract for Validate hooks that change w.After (schema defaults and @write
// resolvers): they mutate w.After in place, and for whole-document writes
// (and a selector PUT that replaces the root element) they also set
// w.Op.Body to the new source bytes, which the engine stores instead of the
// request body.

// SideEffectPut stores a markup document changed as a consequence of w and
// records its PUT mutation event.
func (w *WriteCtx) SideEffectPut(path, contentType string, body []byte) (*store.Document, error) {
	sw := w.sideEffectCtx(path, "PUT")
	stored, err := w.Engine.put(sw, &store.Document{Path: path, ContentType: contentType, Body: body})
	if err != nil {
		return nil, err
	}
	w.Extra = append(w.Extra, stored)
	if err := w.Engine.event(sw, path, sse.Mutation{Method: "PUT", ETag: stored.ETag}); err != nil {
		return nil, err
	}
	return stored, nil
}

// SideEffectDelete deletes a document as a consequence of w and records its
// DELETE mutation event.
func (w *WriteCtx) SideEffectDelete(path string) error {
	sw := w.sideEffectCtx(path, "DELETE")
	if err := w.Tx.Delete(path); err != nil {
		return err
	}
	if w.Op.Plane == Authoring {
		if err := w.Tx.DeleteAuthored(path); err != nil {
			return err
		}
	}
	w.Extra = append(w.Extra, &store.Document{Path: path})
	return w.Engine.event(sw, path, sse.Mutation{Method: "DELETE"})
}

func (w *WriteCtx) sideEffectCtx(path, method string) *WriteCtx {
	// Cascades emit events for every affected document (modeling R-MOD
	// cross-area note); only trigger/processor side effects are silent.
	return &WriteCtx{Site: w.Site, Snap: w.Snap, Tx: w.Tx, Engine: w.Engine, sideEffect: true, cascade: true,
		Op: &Op{Plane: w.Op.Plane, Method: method, Path: path, Host: w.Op.Host, Principal: w.Op.Principal}}
}
