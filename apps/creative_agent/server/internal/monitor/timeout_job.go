package monitor

import (
	"context"
	"time"
)

// RunTimeoutJob is the timeout-fallback job: it scans projects still running after hours hours, refunds points + sets fail.
//
// Aligned with §5.4: works stuck in intermediate states (operator lost contact, process crash) may stay running forever.
// The scheduled scan forces closure, preventing points from being held forever. This is the last-resort fallback — when all
// compensation/retries fail, it guarantees the user's money comes back.
func RunTimeoutJob(ctx context.Context, deps Deps, hours int) error {
	if deps.Projects == nil || deps.PostCreate == nil {
		return nil // dependencies not injected (default assembly scenario); skip
	}
	if hours <= 0 {
		hours = 24
	}
	deadline := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
	stuck, err := deps.Projects.GetStuckProjects(ctx, deadline)
	if err != nil {
		return err
	}
	for _, p := range stuck {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		// RefundAndFailTimeout is internally idempotent: only running is processed, only used points are refunded
		_ = deps.PostCreate.RefundAndFailTimeout(ctx, p)
	}
	return nil
}
