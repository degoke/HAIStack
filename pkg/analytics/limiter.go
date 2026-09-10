package analytics

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// ConcurrencyLimiter bounds concurrent analytics job execution to protect OLTP.
type ConcurrencyLimiter struct {
	sem chan struct{}
}

// NewConcurrencyLimiter returns a limiter that allows at most max concurrent jobs.
// When max <= 0, concurrency is unlimited.
func NewConcurrencyLimiter(max int) *ConcurrencyLimiter {
	if max <= 0 {
		return &ConcurrencyLimiter{}
	}
	return &ConcurrencyLimiter{sem: make(chan struct{}, max)}
}

// Acquire reserves one execution slot. Call the returned function to release it.
func (l *ConcurrencyLimiter) Acquire() func() {
	if l == nil || l.sem == nil {
		return func() {}
	}
	l.sem <- struct{}{}
	return func() { <-l.sem }
}

// LimitedRefreshHandler wraps a refresh handler with concurrency limiting.
func LimitedRefreshHandler(inner jobs.Handler, limiter *ConcurrencyLimiter) jobs.Handler {
	if limiter == nil || limiter.sem == nil {
		return inner
	}
	return jobs.HandlerFunc(func(ctx context.Context, job store.JobRecord) error {
		release := limiter.Acquire()
		defer release()
		return inner.HandleJob(ctx, job)
	})
}
