package jobs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// Runner claims pending jobs from a store.JobStore and dispatches them to
// handlers registered by job type.
type Runner struct {
	Store store.JobStore

	// MaxAttempts is the maximum claim count before a failed job becomes
	// terminal. ClaimNext increments Attempts, so MaxAttempts=1 fails after the
	// first handler error (the historical search/sync default). Zero or negative
	// values default to 1.
	MaxAttempts int

	// Backoff controls RunAfter when a failed job is rescheduled as pending.
	// Ignored when MaxAttempts is 1 (no retry).
	Backoff Backoff

	// PollInterval is the idle sleep for RunLoop when no job is claimed.
	// Defaults to one second.
	PollInterval time.Duration

	// Now supplies the clock; defaults to time.Now UTC.
	Now func() time.Time

	mu       sync.RWMutex
	handlers map[string]Handler
	order    []string
}

// NewRunner constructs a Runner backed by the given JobStore.
func NewRunner(s store.JobStore) *Runner {
	return &Runner{
		Store:       s,
		MaxAttempts: 1,
		Backoff:     DefaultBackoff(),
		handlers:    make(map[string]Handler),
	}
}

// Register associates a job type with a handler. Later registrations replace
// earlier ones for the same type but preserve first-registration claim order.
func (r *Runner) Register(jobType string, handler Handler) error {
	if r == nil {
		return ErrNilStore
	}
	if jobType == "" {
		return ErrEmptyJobType
	}
	if handler == nil {
		return ErrNilHandler
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handlers == nil {
		r.handlers = make(map[string]Handler)
	}
	if _, exists := r.handlers[jobType]; !exists {
		r.order = append(r.order, jobType)
	}
	r.handlers[jobType] = handler
	return nil
}

// RunOnce claims at most one pending job across registered types (in
// registration order) and executes its handler. It returns processed=false when
// no eligible job is available.
func (r *Runner) RunOnce(ctx context.Context) (processed bool, err error) {
	if r == nil || r.Store == nil {
		return false, ErrNilStore
	}

	r.mu.RLock()
	order := append([]string(nil), r.order...)
	handlers := make(map[string]Handler, len(r.handlers))
	for k, v := range r.handlers {
		handlers[k] = v
	}
	r.mu.RUnlock()

	if len(order) == 0 {
		return false, nil
	}

	for _, jobType := range order {
		job, err := r.Store.ClaimNext(ctx, jobType)
		if err != nil {
			return false, err
		}
		if job == nil {
			continue
		}
		handler, ok := handlers[job.Type]
		if !ok {
			now := r.now()
			job.Status = store.JobStatusFailed
			job.LastError = fmt.Sprintf("%v: %s", ErrUnknownJobType, job.Type)
			job.UpdatedAt = now
			if updateErr := r.Store.Update(ctx, *job); updateErr != nil {
				return true, updateErr
			}
			return true, fmt.Errorf("%w: %s", ErrUnknownJobType, job.Type)
		}
		handleErr := handler.HandleJob(ctx, *job)
		if applyErr := ApplyHandlerResult(ctx, r.Store, job, handleErr, ApplyOptions{
			MaxAttempts: r.maxAttempts(),
			Backoff:     r.backoff(),
			Now:         r.now,
		}); applyErr != nil {
			return true, applyErr
		}
		return true, handleErr
	}
	return false, nil
}

// RunLoop repeatedly calls RunOnce until ctx is cancelled. Idle polls sleep for
// PollInterval.
func (r *Runner) RunLoop(ctx context.Context) error {
	if r == nil || r.Store == nil {
		return ErrNilStore
	}
	interval := r.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		processed, err := r.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			// Surface handler/store errors but keep looping unless cancelled.
			// Callers that want fail-fast can use RunOnce.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(interval):
			}
			continue
		}
		if processed {
			continue
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *Runner) now() time.Time {
	if r != nil && r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *Runner) maxAttempts() int {
	if r == nil || r.MaxAttempts <= 0 {
		return 1
	}
	return r.MaxAttempts
}

func (r *Runner) backoff() Backoff {
	if r == nil || r.Backoff.Base <= 0 {
		return DefaultBackoff()
	}
	return r.Backoff
}
