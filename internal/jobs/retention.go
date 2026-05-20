package jobs

import (
	"context"
	"time"
)

type retentionStore interface {
	DeleteRequestLogsBefore(ctx context.Context, cutoff time.Time) error
}

func StartRetentionLoop(ctx context.Context, store retentionStore, keep time.Duration, every time.Duration) {
	if store == nil || keep <= 0 || every <= 0 {
		return
	}

	ticker := time.NewTicker(every)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = store.DeleteRequestLogsBefore(ctx, time.Now().Add(-keep))
			}
		}
	}()
}
