package engine

import (
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

// publish hands committed events to the site's broker. Each event reaches
// the subscribers of its own path and of the pages composed from it, which
// watch it as a source (R-SSE-61).
func publish(s *site.Site, _ *site.Snapshot, events []store.Event) {
	s.Broker.Publish(events)
}
