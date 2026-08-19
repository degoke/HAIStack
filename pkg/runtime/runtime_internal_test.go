package runtime

import (
	"context"
	"errors"
	"testing"
)

func TestRuntimeShutdownReturnsBackgroundWorkerError(t *testing.T) {
	rt := &Runtime{}
	rt.recordBackgroundError(errors.Join(ErrBackgroundWorker, errors.New("background failure")))

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	err := rt.Shutdown(context.Background())
	if !errors.Is(err, ErrBackgroundWorker) {
		t.Fatalf("Shutdown err = %v, want ErrBackgroundWorker", err)
	}
}

func TestRuntimeShutdownCleansUpAfterCallerContextExpires(t *testing.T) {
	cleaned := false
	rt := &Runtime{}
	rt.cleanup.add(func() { cleaned = true })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rt.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if !cleaned {
		t.Fatal("cleanup stack did not run after canceled shutdown context")
	}
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}
