package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestInMemoryJobStoreEnqueueClaimUpdateGet(t *testing.T) {
	ctx := context.Background()
	fixed := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	s := NewInMemoryJobStore()
	s.Now = func() time.Time { return fixed }

	job, err := NewJob(TypeReindex, map[string]string{"resourceType": "Patient"}, EnqueueOptions{
		ID:  "job-1",
		Now: func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if err := s.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := s.Enqueue(ctx, job); !errors.Is(err, ErrDuplicateJob) {
		t.Fatalf("duplicate err = %v, want ErrDuplicateJob", err)
	}

	claimed, err := s.ClaimNext(ctx, TypeReindex)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected claimed job")
	}
	if claimed.Status != store.JobStatusRunning {
		t.Fatalf("status = %q, want running", claimed.Status)
	}
	if claimed.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", claimed.Attempts)
	}

	none, err := s.ClaimNext(ctx, TypeReindex)
	if err != nil || none != nil {
		t.Fatalf("second claim = %#v err=%v", none, err)
	}

	claimed.Status = store.JobStatusCompleted
	claimed.UpdatedAt = fixed
	if err := s.Update(ctx, *claimed); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := s.Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != store.JobStatusCompleted {
		t.Fatalf("status = %q", got.Status)
	}
}

func TestInMemoryJobStoreRunAfterGating(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	s := NewInMemoryJobStore()
	s.Now = func() time.Time { return now }

	job, err := NewJob("demo", nil, EnqueueOptions{
		ID:       "future",
		RunAfter: now.Add(time.Hour),
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if err := s.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, err := s.ClaimNext(ctx, "demo")
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed != nil {
		t.Fatalf("expected nil claim for future RunAfter, got %#v", claimed)
	}

	s.Now = func() time.Time { return now.Add(2 * time.Hour) }
	claimed, err = s.ClaimNext(ctx, "demo")
	if err != nil || claimed == nil {
		t.Fatalf("ClaimNext after RunAfter: %#v err=%v", claimed, err)
	}
}

func TestRunnerDispatchByType(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryJobStore()
	runner := NewRunner(s)

	var seen string
	if err := runner.Register("alpha", HandlerFunc(func(_ context.Context, job store.JobRecord) error {
		seen = job.Type
		return nil
	})); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := runner.Register("beta", HandlerFunc(func(context.Context, store.JobRecord) error {
		t.Fatal("beta should not run")
		return nil
	})); err != nil {
		t.Fatalf("Register beta: %v", err)
	}

	if _, err := Enqueue(ctx, s, "alpha", nil, EnqueueOptions{ID: "a1"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	processed, err := runner.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("RunOnce: processed=%v err=%v", processed, err)
	}
	if seen != "alpha" {
		t.Fatalf("seen = %q", seen)
	}

	processed, err = runner.RunOnce(ctx)
	if err != nil || processed {
		t.Fatalf("empty RunOnce: processed=%v err=%v", processed, err)
	}
}

func TestRunnerRetryBackoffReschedule(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	s := NewInMemoryJobStore()
	s.Now = func() time.Time { return now }

	runner := NewRunner(s)
	runner.MaxAttempts = 3
	runner.Backoff = Backoff{Base: 10 * time.Second, Factor: 2, Max: time.Minute}
	runner.Now = func() time.Time { return now }

	var calls atomic.Int32
	if err := runner.Register("retry.me", HandlerFunc(func(context.Context, store.JobRecord) error {
		calls.Add(1)
		return errors.New("boom")
	})); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, err := Enqueue(ctx, s, "retry.me", nil, EnqueueOptions{ID: "r1", Now: func() time.Time { return now }}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	processed, err := runner.RunOnce(ctx)
	if !processed {
		t.Fatal("expected processed")
	}
	if err == nil {
		t.Fatal("expected handler error")
	}
	got, err := s.Get(ctx, "r1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != store.JobStatusPending {
		t.Fatalf("status = %q, want pending", got.Status)
	}
	if got.Attempts != 1 {
		t.Fatalf("attempts = %d", got.Attempts)
	}
	if !got.RunAfter.Equal(now.Add(10 * time.Second)) {
		t.Fatalf("runAfter = %v, want %v", got.RunAfter, now.Add(10*time.Second))
	}

	// Still gated by RunAfter.
	processed, err = runner.RunOnce(ctx)
	if err != nil || processed {
		t.Fatalf("gated RunOnce: processed=%v err=%v", processed, err)
	}

	now = now.Add(11 * time.Second)
	processed, err = runner.RunOnce(ctx)
	if !processed || err == nil {
		t.Fatalf("second attempt: processed=%v err=%v", processed, err)
	}
	got, _ = s.Get(ctx, "r1")
	if got.Status != store.JobStatusPending {
		t.Fatalf("status after 2nd fail = %q", got.Status)
	}

	now = got.RunAfter.Add(time.Second)
	processed, err = runner.RunOnce(ctx)
	if !processed || err == nil {
		t.Fatalf("third attempt: processed=%v err=%v", processed, err)
	}
	got, _ = s.Get(ctx, "r1")
	if got.Status != store.JobStatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestRunnerNoJobAndUnknownType(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryJobStore()
	runner := NewRunner(s)
	if err := runner.Register("known", HandlerFunc(func(context.Context, store.JobRecord) error {
		return nil
	})); err != nil {
		t.Fatalf("Register: %v", err)
	}

	processed, err := runner.RunOnce(ctx)
	if err != nil || processed {
		t.Fatalf("no-job: processed=%v err=%v", processed, err)
	}

	// Fake store returns a claimed job whose Type differs from the registered name.
	fake := &mismatchClaimStore{inner: s, claimAs: "other"}
	runner.Store = fake
	if _, err := Enqueue(ctx, s, "known", nil, EnqueueOptions{ID: "u1"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	processed, err = runner.RunOnce(ctx)
	if !processed || !errors.Is(err, ErrUnknownJobType) {
		t.Fatalf("unknown type: processed=%v err=%v", processed, err)
	}
	got, getErr := s.Get(ctx, "u1")
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if got.Status != store.JobStatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
}

// mismatchClaimStore claims under the registered type but rewrites job.Type.
type mismatchClaimStore struct {
	inner   store.JobStore
	claimAs string
}

func (s *mismatchClaimStore) Enqueue(ctx context.Context, job store.JobRecord) error {
	return s.inner.Enqueue(ctx, job)
}
func (s *mismatchClaimStore) ClaimNext(ctx context.Context, jobType string) (*store.JobRecord, error) {
	job, err := s.inner.ClaimNext(ctx, jobType)
	if job != nil {
		job.Type = s.claimAs
	}
	return job, err
}
func (s *mismatchClaimStore) Update(ctx context.Context, job store.JobRecord) error {
	return s.inner.Update(ctx, job)
}
func (s *mismatchClaimStore) Get(ctx context.Context, id string) (*store.JobRecord, error) {
	return s.inner.Get(ctx, id)
}

func TestNextRunAfterAndShouldRetry(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := NextRunAfter(1, now, Backoff{Base: time.Second, Factor: 2, Max: time.Minute})
	if !got.Equal(now.Add(time.Second)) {
		t.Fatalf("attempt1 = %v", got)
	}
	got = NextRunAfter(3, now, Backoff{Base: time.Second, Factor: 2, Max: time.Minute})
	if !got.Equal(now.Add(4 * time.Second)) {
		t.Fatalf("attempt3 = %v", got)
	}
	if ShouldRetry(1, 1) {
		t.Fatal("ShouldRetry(1,1) want false")
	}
	if !ShouldRetry(1, 2) {
		t.Fatal("ShouldRetry(1,2) want true")
	}
}

func TestEnqueueTypedPayload(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryJobStore()
	type payload struct {
		Name string `json:"name"`
	}
	job, err := Enqueue(ctx, s, TypeReindex, payload{Name: "Patient"}, EnqueueOptions{ID: "p1"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	var out payload
	if err := UnmarshalPayload(job.Payload, &out); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}
	if out.Name != "Patient" {
		t.Fatalf("name = %q", out.Name)
	}
}
