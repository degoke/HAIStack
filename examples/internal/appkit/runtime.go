package appkit

import (
	"context"
	"errors"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/runtime"
)

// ShutdownRuntime normalizes benign runtime shutdown errors for examples.
func ShutdownRuntime(ctx context.Context, rt *runtime.Runtime) error {
	if rt == nil {
		return nil
	}
	err := rt.Shutdown(ctx)
	if err == nil {
		return nil
	}
	if errors.Is(err, runtime.ErrBackgroundWorker) && strings.Contains(err.Error(), "context canceled") {
		return nil
	}
	return err
}
