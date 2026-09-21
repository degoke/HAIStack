package oauth

import (
	"context"
	"time"
)

// DefaultPendingAuthorizationCleanupInterval is the recommended background purge
// interval for SQL-backed consent sessions in production-like hosts.
const DefaultPendingAuthorizationCleanupInterval = 5 * time.Minute

// RunPendingAuthorizationCleanup periodically purges expired consent sessions until ctx is cancelled.
func RunPendingAuthorizationCleanup(ctx context.Context, store AuthorizationStore, interval time.Duration) {
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
			store.PurgeExpiredPendingAuthorizations()
		}
	}
}

// StartPendingAuthorizationCleanup runs RunPendingAuthorizationCleanup in a background goroutine.
func StartPendingAuthorizationCleanup(ctx context.Context, store AuthorizationStore, interval time.Duration) {
	go RunPendingAuthorizationCleanup(ctx, store, interval)
}
