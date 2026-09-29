package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/sse"
	"github.com/sky-valley/pagelike/internal/store"
)

// Stream timing (PageLove docs, Server-Sent Events).
var (
	KeepaliveInterval = 20 * time.Second
	Retention         = 10 * time.Minute
	// SessionCheckInterval is how often a signed-in subscriber's session is
	// re-checked for expiry or invalidation (docs/spec/sse.md R-SSE-34).
	SessionCheckInterval = time.Second
)

// subscribe serves an SSE stream for one document path (docs/spec/sse.md):
// the path a GET would serve (query ignored, a directory URL means its
// index, a slash-less directory gets the GET's 301), authorized like a
// whole-document GET except that the default-GET mode never grants it. A
// path with no document yet is subscribed to like any other (live
// 2026-09-28, superseding R-SSE-4's 404): the stream carries its writes.
func (p *Public) subscribe(w http.ResponseWriter, r *http.Request, rc *reqCtx, clean string) {
	ctx := r.Context()
	s := rc.site
	snap, err := s.Index(ctx)
	if err != nil {
		p.fail(w, r, rc, err)
		return
	}
	docPath := engine.DocPath(clean)
	_, err = s.Store.Get(ctx, docPath)
	missing := errors.Is(err, store.ErrNotFound)
	if err != nil && !missing {
		p.fail(w, r, rc, err)
		return
	}
	if missing && p.redirectDirectory(w, r, rc, snap, clean) {
		return
	}
	req := p.authzReq(r, rc, http.MethodGet, docPath)
	req.Subscribe = true
	if !snap.Policy.Decide(req, nil).Allowed {
		p.fail(w, r, rc, engine.Denied(rc.principal, docPath))
		return
	}
	ctl := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	sub := s.Broker.SubscribeN(docPath, rc.principal.Session, s.Settings().SSEBufferEvents)
	defer s.Broker.Unsubscribe(sub)
	done, watched := make(chan struct{}), make(chan struct{})
	go func() {
		// A subscriber dropped for not reading may be blocked in a write
		// that will never finish; end it so the connection closes and the
		// client reconnects with Last-Event-ID (R-SSE-36). The expired
		// deadline also makes the response's final write fail, so the
		// connection is not reused.
		defer close(watched)
		select {
		case <-sub.Dropped:
			ctl.SetWriteDeadline(time.Now())
		case <-done:
		}
	}()
	defer func() {
		close(done)
		<-watched // never touch the connection after the handler returns
	}()

	st := &stream{p: p, r: r, rc: rc, docPath: docPath, sub: sub, w: w}
	// A ": connected" comment, then the token event, with no id (R-SSE-6,
	// live 2026-09-28).
	if _, err := io.WriteString(w, sse.Connected); err != nil {
		return
	}
	if err := sse.WriteEvent(w, "", "pagelove-connection", sub.Conn); err != nil {
		return
	}
	last := int64(-1) // highest sequence number already handled
	if id := strings.TrimSpace(r.Header.Get("Last-Event-ID")); id != "" {
		_, seq, ok := sse.ParseEventID(id)
		switch {
		case !ok:
			// An id not of the server's shape is ignored: the stream is
			// live only, with no reset (live 2026-09-28, R-SSE-32).
		case eventsLost(ctx, s, seq):
			// Events after the id are gone (retention); the client
			// refetches and the stream carries on live (R-SSE-32).
			if sse.WriteEvent(w, "", "reset", sse.Reset(sse.ResetEventsExpired)) != nil {
				return
			}
		default:
			// Everything retained after the id is replayed, however old the
			// id is (live 2026-09-29: an id from 1970 replayed the
			// document's events rather than resetting).
			last = seq
			evs, err := s.Store.EventsAfter(ctx, docPath, seq)
			if err != nil {
				return
			}
			for _, ev := range evs {
				last = ev.Seq
				if !st.deliver(ctx, ev) {
					return
				}
			}
		}
	}
	if err := ctl.Flush(); err != nil {
		return
	}

	keepalive := time.NewTicker(KeepaliveInterval)
	defer keepalive.Stop()
	var sessionTick <-chan time.Time
	if pr := rc.principal; pr != nil && pr.Authenticated && pr.Session != "" && !strings.HasPrefix(pr.Session, "dev-") {
		t := time.NewTicker(SessionCheckInterval)
		defer t.Stop()
		sessionTick = t.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Dropped:
			return
		case <-keepalive.C:
			if _, err := w.Write([]byte(sse.Keepalive)); err != nil {
				return
			}
			if ctl.Flush() != nil {
				return
			}
		case <-sessionTick:
			if p.CheckSession != nil && !p.CheckSession(ctx, s, rc.principal.Session) {
				return
			}
			if reason := sessionReset(ctx, s, rc.principal.Session); reason != "" {
				sse.WriteEvent(w, "", "reset", sse.Reset(reason))
				return
			}
		case ev := <-sub.C:
			sub.Received(ev)
			if ev.Seq <= last {
				continue // already replayed
			}
			select {
			case <-sub.Dropped:
				return
			default:
			}
			if !st.deliver(ctx, ev) {
				return
			}
		}
	}
}

// stream is one open subscription's delivery state.
type stream struct {
	p       *Public
	r       *http.Request
	rc      *reqCtx
	docPath string
	sub     *sse.Subscriber
	w       http.ResponseWriter
}

// deliver writes ev unless echo suppression or read authorization withholds
// it; it reports false when the stream must end (write failed, or the
// subscriber may no longer subscribe).
func (st *stream) deliver(ctx context.Context, ev store.Event) bool {
	if sse.Suppressed(st.sub, ev) {
		return true
	}
	ok, keep := st.readable(ctx, ev)
	if !keep {
		return false
	}
	if !ok {
		return true
	}
	return sse.WriteStored(st.w, ev) == nil
}

// readable re-checks read access with the current rules before an event is
// delivered (docs/spec/sse.md R-SSE-28): a subscriber who may no longer
// subscribe loses the stream; an event about an element that a GET would
// deny them (a selector-scoped rule) is withheld. Replayed events carry no
// elements, so they are withheld whenever the subscriber's access depends
// on elements at all (fail closed).
func (st *stream) readable(ctx context.Context, ev store.Event) (ok, keep bool) {
	if st.rc.principal.Authenticated && st.p.CheckSession != nil && !st.p.CheckSession(ctx, st.rc.site, st.rc.principal.Session) {
		return false, false
	}
	snap, err := st.rc.site.Index(ctx)
	if err != nil {
		return false, false
	}
	pol := snap.Policy
	req := st.p.authzReq(st.r, st.rc, http.MethodGet, st.docPath)
	sreq := req
	sreq.Subscribe = true
	if !pol.Decide(sreq, nil).Allowed {
		return false, false
	}
	if sse.WholeDocument(ev.Data) {
		return true, true
	}
	if len(ev.Targets) == 0 {
		return !elementDependent(pol, req), true
	}
	for _, t := range ev.Targets {
		if !pol.Decide(req, t).Allowed {
			return false, true
		}
	}
	return true, true
}

// elementDependent reports whether some selector-scoped rule changes the
// principal's read access to part of the document.
func elementDependent(pol *authz.Policy, req authz.Request) bool {
	doc := pol.DecideText(req, "").Allowed
	for _, sel := range pol.SelectorRules(req) {
		if pol.DecideText(req, sel).Allowed != doc {
			return true
		}
	}
	return false
}

// eventsLost reports whether any event after position seq has been pruned,
// so a replay from seq would have a gap. Expired events are pruned first
// (the janitor may not have run yet). Event positions are one site-wide
// sequence, so a gap anywhere after seq counts (conservative: reset).
func eventsLost(ctx context.Context, s *site.Site, seq int64) bool {
	s.Store.PruneEvents(ctx, time.Now().Add(-Retention))
	db := s.Store.DB()
	var minSeq sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MIN(seq) FROM events`).Scan(&minSeq); err != nil {
		return false
	}
	if minSeq.Valid {
		return minSeq.Int64 > seq+1
	}
	// No event retained: lost if any was ever assigned after seq.
	var lastSeq sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT seq FROM sqlite_sequence WHERE name = 'events'`).Scan(&lastSeq); err != nil {
		return false
	}
	return lastSeq.Valid && lastSeq.Int64 > seq
}

// sessionReset reports the reset reason when a signed-in subscriber's
// session has ended: "session-invalidated" once it is gone (logout),
// "session-expired" once its expiry has passed, "" while it is valid or
// cannot be checked. Sessions belong to the identity package; this is a
// read-only look at its table because it offers no expiry lookup.
func sessionReset(ctx context.Context, s *site.Site, id string) string {
	var exp int64
	err := s.Store.DB().QueryRowContext(ctx, `SELECT expires_ms FROM sessions WHERE id = ?`, id).Scan(&exp)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return sse.ResetSessionInvalidated
	case err != nil:
		return ""
	case exp <= time.Now().UnixMilli():
		return sse.ResetSessionExpired
	}
	return ""
}
