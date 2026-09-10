package oauth

import (
	"context"
	"time"
)

// DefaultConsentSessionCleanupInterval is the recommended background purge interval
// for SQLite-backed consent sessions in production-like hosts.
const DefaultConsentSessionCleanupInterval = 5 * time.Minute

// RunConsentSessionCleanup periodically purges expired consent sessions until ctx is cancelled.
// interval must be positive; store must not be nil (no-op otherwise).
func RunConsentSessionCleanup(ctx context.Context, store ConsentSessionStore, interval time.Duration) {
	if store == nil || interval <= 0 {
		<-ctx.Done()
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = store.PurgeExpired(ctx)
		}
	}
}

// StartConsentSessionCleanup runs RunConsentSessionCleanup in a background goroutine.
func StartConsentSessionCleanup(ctx context.Context, store ConsentSessionStore, interval time.Duration) {
	go RunConsentSessionCleanup(ctx, store, interval)
}
