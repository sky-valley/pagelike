package hosting

import (
	"context"
	"errors"
	"time"
)

// PruneTemporary removes only expired disposable sites. Published sites,
// including terminally removed sites, remain retained for operator policy.
func (h *Server) PruneTemporary(ctx context.Context, now time.Time) error {
	h.installMu.Lock()
	defer h.installMu.Unlock()
	names, err := h.core.Sites.List()
	if err != nil {
		return err
	}
	var failures []error
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		s, err := h.core.Sites.Get(ctx, name)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		current, err := state(ctx, s)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if current.Temporary && current.ExpiresAt > 0 && current.ExpiresAt <= now.UnixMilli() {
			failures = append(failures, h.core.Sites.Delete(name))
		}
	}
	return errors.Join(failures...)
}
