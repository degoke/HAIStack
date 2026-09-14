package runtime

import "context"

// ConformanceRefresher rebuilds the live registry snapshot, validation catalog,
// and related conformance consumers after IG or terminology installs.
type ConformanceRefresher interface {
	Refresh(ctx context.Context) error
}

type conformanceRuntimeRefresher struct {
	runtime *ConformanceRuntime
	onDone  func()
}

func (c conformanceRuntimeRefresher) Refresh(ctx context.Context) error {
	snap, err := c.runtime.Refresh(ctx)
	if err != nil {
		return err
	}
	if c.onDone != nil {
		c.onDone()
	} else if snap != nil {
		_ = snap
	}
	return nil
}

// NewConformanceRefresher adapts ConformanceRuntime to ConformanceRefresher.
func NewConformanceRefresher(runtime *ConformanceRuntime, onDone func()) ConformanceRefresher {
	if runtime == nil {
		return nil
	}
	return conformanceRuntimeRefresher{runtime: runtime, onDone: onDone}
}
