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
