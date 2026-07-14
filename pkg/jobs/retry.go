package jobs

import (
	"context"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// Backoff configures exponential delay between retry attempts.
type Backoff struct {
	// Base is the delay after the first failed attempt.
	Base time.Duration
	// Factor multiplies the delay per subsequent attempt. Values <= 1 act as 2.
	Factor float64
	// Max caps the computed delay.
	Max time.Duration
}

// DefaultBackoff returns a one-second base, factor 2, one-minute cap.
func DefaultBackoff() Backoff {
	return Backoff{
		Base:   time.Second,
		Factor: 2,
		Max:    time.Minute,
	}
}

// NextRunAfter computes the next RunAfter timestamp for the given 1-based
// attempt count (typically job.Attempts after ClaimNext).
func NextRunAfter(attempts int, now time.Time, policy Backoff) time.Time {
	if policy.Base <= 0 {
		policy = DefaultBackoff()
	}
	if policy.Factor <= 1 {
		policy.Factor = 2
	}
	if policy.Max <= 0 {
		policy.Max = time.Minute
	}
	if attempts < 1 {
		attempts = 1
	}
	delay := policy.Base
	for i := 1; i < attempts; i++ {
		delay = time.Duration(float64(delay) * policy.Factor)
		if delay > policy.Max {
			delay = policy.Max
			break
		}
	}
	if delay > policy.Max {
		delay = policy.Max
	}
	return now.UTC().Add(delay)
}

// ShouldRetry reports whether another attempt is allowed.
func ShouldRetry(attempts, maxAttempts int) bool {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	return attempts < maxAttempts
}

// ApplyOptions configure ApplyHandlerResult.
type ApplyOptions struct {
	MaxAttempts int
	Backoff     Backoff
	Now         func() time.Time
}

// ApplyHandlerResult updates the job after a handler returns. On success the
// job is marked completed. On failure it is either rescheduled as pending with
// RunAfter when ShouldRetry is true, or marked failed.
func ApplyHandlerResult(ctx context.Context, jobs store.JobStore, job *store.JobRecord, handleErr error, opts ApplyOptions) error {
	if jobs == nil || job == nil {
		return ErrNilStore
	}
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	backoff := opts.Backoff
	if backoff.Base <= 0 {
		backoff = DefaultBackoff()
	}

	job.UpdatedAt = now
	if handleErr == nil {
		MarkCompleted(job, now)
	} else if ShouldRetry(job.Attempts, maxAttempts) {
		ReschedulePending(job, handleErr, now, backoff)
	} else {
		MarkFailed(job, handleErr, now)
	}
	return jobs.Update(ctx, *job)
}

// MarkCompleted sets terminal success fields on job.
func MarkCompleted(job *store.JobRecord, now time.Time) {
	if job == nil {
		return
	}
	job.Status = store.JobStatusCompleted
	job.LastError = ""
	job.UpdatedAt = now.UTC()
	job.RunAfter = time.Time{}
}

// MarkFailed sets terminal failure fields on job.
func MarkFailed(job *store.JobRecord, err error, now time.Time) {
	if job == nil {
		return
	}
	job.Status = store.JobStatusFailed
	if err != nil {
		job.LastError = err.Error()
	}
	job.UpdatedAt = now.UTC()
	job.RunAfter = time.Time{}
}

// ReschedulePending returns a failed claim to pending with backoff RunAfter.
func ReschedulePending(job *store.JobRecord, err error, now time.Time, policy Backoff) {
	if job == nil {
		return
	}
	job.Status = store.JobStatusPending
	if err != nil {
		job.LastError = err.Error()
	}
	job.UpdatedAt = now.UTC()
	job.RunAfter = NextRunAfter(job.Attempts, now, policy)
}
