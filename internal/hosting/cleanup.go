package hosting

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
			failures = append(failures, fmt.Errorf("site %s: %w", name, err))
			continue
		}
		current, err := state(ctx, s)
		if errors.Is(err, sql.ErrNoRows) {
			continue // never installed: not a hosted site, nothing to prune
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("site %s: %w", name, err))
			continue
		}
		if current.Temporary && current.ExpiresAt > 0 && current.ExpiresAt <= now.UnixMilli() {
			if err := h.core.Sites.Delete(name); err != nil {
				failures = append(failures, fmt.Errorf("site %s: %w", name, err))
			}
		}
	}
	return errors.Join(failures...)
}
