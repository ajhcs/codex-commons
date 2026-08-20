package opsfs

import (
	"context"
	"os"
	"time"
)

const (
	HoldPreOpen                  = "pre-open"
	HoldPrePublish               = "pre-publish"
	HoldPrePublishRename         = "pre-publish-rename"
	HoldBetweenPublications      = "between-publications"
	HoldAfterPublications        = "after-publications"
	HoldAfterMonthlyPublications = "after-monthly-publications"
	HoldPreUnlink                = "pre-unlink"
)

// WaitHold is a disposable-test seam. It is inert unless both
// COMMONS_OPS_HOLD and COMMONS_OPS_HOLD_POINT are set. A same-uid actor
// can already SIGSTOP the process; this does not enlarge that threat.
func WaitHold(ctx context.Context, point string) error {
	if os.Getenv("COMMONS_OPS_HOLD_POINT") != point {
		return nil
	}
	path := os.Getenv("COMMONS_OPS_HOLD")
	if path == "" {
		return nil
	}
	if err := ValidAbsPath(path); err != nil {
		return err
	}
	if status := os.Getenv("COMMONS_OPS_HOLD_STATUS"); status != "" {
		if err := ValidAbsPath(status); err == nil {
			_ = os.WriteFile(status, []byte(point+"\n"), FileMode)
		}
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Lstat(path); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
