package engine

import (
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

// EventAudience returns further document paths whose live subscribers also
// receive the events of path: the pages that include it. Live PageLove
// delivers a change to an included document to subscribers of the including
// page as well (2026-09-28, decisions.md sse.scope.include-not-propagated),
// contradicting the documented "composed resources" rule. Set by the
// composition package; nil means events reach only their own path.
var EventAudience func(snap *site.Snapshot, path string) []string

// publish hands committed events to the site's broker, widening each
// event's audience to the pages that include its document.
func publish(s *site.Site, snap *site.Snapshot, events []store.Event) {
	if EventAudience != nil && snap != nil {
		for i := range events {
			events[i].Also = EventAudience(snap, events[i].Path)
		}
	}
	s.Broker.Publish(events)
}
