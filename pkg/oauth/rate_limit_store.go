package oauth

import (
	"context"
	"time"
)

// RateLimitStore persists OAuth endpoint rate limit counters across processes.
type RateLimitStore interface {
	Allow(ctx context.Context, endpoint, bucketKey string, limit int, window time.Duration, now time.Time) (bool, error)
}
