package sync

import (
	"context"
	"errors"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/conflict"
)

type failingResolutionHandler struct {
	err error
}

func (h failingResolutionHandler) OnConflictResolution(context.Context, ConflictJobPayload, conflict.MergeResult) error {
	return h.err
}

func TestNotifyResolutionHandlerPropagatesErrors(t *testing.T) {
	want := errors.New("resolution failed")
	processor := &JobProcessor{}
	err := processor.notifyResolutionHandler(
		context.Background(),
		Config{ConflictResolutionHandler: failingResolutionHandler{err: want}},
		ConflictJobPayload{},
		conflict.MergeResult{},
	)
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
