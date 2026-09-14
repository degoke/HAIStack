package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/view"
)

// IncrementalTarget wraps a ReportingTarget with watermark-based delta refresh.
// Watermarks are advanced by RefreshHandler after a successful refresh completes;
// WriteAt persists rows only.
type IncrementalTarget struct {
	target    *ReportingTarget
	watermark *WatermarkStore
}

// NewIncrementalTarget returns a reporting target that reads _since from WatermarkStore.
func NewIncrementalTarget(target *ReportingTarget, watermark *WatermarkStore) *IncrementalTarget {
	return &IncrementalTarget{
		target:    target,
		watermark: watermark,
	}
}

// Since returns the last successful refresh watermark for a view, if any.
func (t *IncrementalTarget) Since(ctx context.Context, viewName, version string) (time.Time, error) {
	if t == nil || t.watermark == nil {
		return time.Time{}, nil
	}
	return t.watermark.Since(ctx, viewName, version)
}

// WriteAt persists rows without advancing the watermark.
func (t *IncrementalTarget) WriteAt(ctx context.Context, result *view.Result, refreshedAt time.Time) error {
	if t == nil || t.target == nil {
		return fmt.Errorf("%w: reporting target is required", ErrUnsupportedDestination)
	}
	return t.target.writeAt(ctx, result, refreshedAt)
}

func (t *IncrementalTarget) writeAt(ctx context.Context, result *view.Result, refreshedAt time.Time) error {
	return t.WriteAt(ctx, result, refreshedAt)
}
