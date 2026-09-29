// Package sse fans committed mutation events out to Server-Sent Events
// subscribers of a site.
//
// Semantics (PageLove docs, Server-Sent Events, snapshot 2026-09-28;
// docs/spec/sse.md):
//   - a subscription covers exactly one document path;
//   - the first event on every stream is "pagelove-connection" carrying an
//     opaque server-assigned connection token;
//   - a mutation is never delivered back to its writer: with a
//     Pagelove-Connection token on the write only the stream holding that
//     token is skipped, whoever presents it (live 2026-09-28, superseding
//     the session-and-token reading of R-SSE-38), otherwise every stream of
//     the writer's session is (R-SSE-37);
//   - each subscriber has a bounded buffer; a subscriber that stops draining
//     is disconnected and recovers through Last-Event-ID replay (R-SSE-36).
package sse

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"sync/atomic"

	"github.com/sky-valley/pagelike/internal/store"
)

// A subscriber's queue of undelivered events is bounded by count and by
// payload bytes, whichever fills first (docs/spec/sse.md R-SSE-36).
const (
	BufferSize  = 256
	BufferBytes = 4 << 20
)

// Subscriber is one open stream.
type Subscriber struct {
	Path    string
	Session string
	Conn    string
	C       chan store.Event
	// Dropped is closed when the broker disconnects a slow subscriber.
	Dropped chan struct{}
	once    sync.Once
	queued  atomic.Int64 // payload bytes sent to C and not yet Received
}

func (s *Subscriber) drop() { s.once.Do(func() { close(s.Dropped) }) }

// Received releases an event taken from C from the byte bound.
func (s *Subscriber) Received(ev store.Event) { s.queued.Add(-int64(len(ev.Data))) }

// Broker holds a site's live subscriptions.
type Broker struct {
	mu   sync.Mutex
	subs map[string]map[*Subscriber]struct{}
}

// NewBroker returns an empty broker.
func NewBroker() *Broker { return &Broker{subs: map[string]map[*Subscriber]struct{}{}} }

// NewToken returns a fresh opaque connection token, a random (version 4)
// UUID as PageLove issues them (live 2026-09-28): 122 random bits, usable
// verbatim as a header value.
func NewToken() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// Subscribe registers a stream for path with the default buffer.
func (b *Broker) Subscribe(path, session string) *Subscriber {
	return b.SubscribeN(path, session, BufferSize)
}

// SubscribeN registers a stream with an n-event buffer.
func (b *Broker) SubscribeN(path, session string, n int) *Subscriber {
	if n <= 0 {
		n = BufferSize
	}
	s := &Subscriber{Path: path, Session: session, Conn: NewToken(), C: make(chan store.Event, n), Dropped: make(chan struct{})}
	b.mu.Lock()
	m := b.subs[path]
	if m == nil {
		m = map[*Subscriber]struct{}{}
		b.subs[path] = m
	}
	m[s] = struct{}{}
	b.mu.Unlock()
	return s
}

// Unsubscribe removes a stream.
func (b *Broker) Unsubscribe(s *Subscriber) {
	b.mu.Lock()
	if m := b.subs[s.Path]; m != nil {
		delete(m, s)
		if len(m) == 0 {
			delete(b.subs, s.Path)
		}
	}
	b.mu.Unlock()
}

// Suppressed reports whether ev must not be delivered to s (echo
// suppression, applied alike to live and replayed events). A token on the
// write names the one stream to skip, whatever session presents it (live
// 2026-09-28: PageLove honours a token presented by another session), so a
// stale or unknown token suppresses nothing; without a token every stream
// of the writer's session is skipped. Writes without a session
// (server-originated) reach everyone.
func Suppressed(s *Subscriber, ev store.Event) bool {
	if ev.OriginConn != "" {
		return ev.OriginConn == s.Conn
	}
	return ev.OriginSession != "" && ev.OriginSession == s.Session
}

// Publish delivers committed events to live subscribers without blocking.
func (b *Broker) Publish(events []store.Event) {
	if len(events) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ev := range events {
		seen := map[*Subscriber]bool{}
		for _, p := range append([]string{ev.Path}, ev.Also...) {
			for s := range b.subs[p] {
				if seen[s] {
					continue
				}
				seen[s] = true
				if Suppressed(s, ev) {
					continue
				}
				n := int64(len(ev.Data))
				if s.queued.Load()+n > BufferBytes {
					delete(b.subs[p], s)
					s.drop()
					continue
				}
				select {
				case s.C <- ev:
					s.queued.Add(n)
				default:
					// Slow consumer: disconnect; it will replay via Last-Event-ID.
					delete(b.subs[p], s)
					s.drop()
				}
			}
		}
	}
}

// Count returns the number of live subscribers (for metrics/tests).
func (b *Broker) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, m := range b.subs {
		n += len(m)
	}
	return n
}

// CloseAll drops every subscriber (used on shutdown).
func (b *Broker) CloseAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for p, m := range b.subs {
		for s := range m {
			s.drop()
		}
		delete(b.subs, p)
	}
}
