package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/view"
)

const cursorPrefix = "analytics.view."

// IncrementalTarget wraps a ReportingTarget with cursor-based delta refresh.
type IncrementalTarget struct {
	target *ReportingTarget
	cursor store.CursorStore
	now    func() time.Time
}

// NewIncrementalTarget returns a reporting target that tracks refresh cursors.
func NewIncrementalTarget(target *ReportingTarget, cursor store.CursorStore) *IncrementalTarget {
	return &IncrementalTarget{
		target: target,
		now:    time.Now,
		cursor: cursor,
	}
}

// Since returns the last successful refresh timestamp for a view, if any.
func (t *IncrementalTarget) Since(ctx context.Context, viewName, version string) (time.Time, error) {
	if t == nil || t.cursor == nil {
		return time.Time{}, nil
	}
	record, err := t.cursor.GetCursor(ctx, cursorName(viewName, version))
	if err != nil || record == nil || record.Position == "" {
		return time.Time{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, record.Position)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse analytics cursor: %w", err)
	}
	return parsed.UTC(), nil
}

// WriteAt persists rows and advances the refresh cursor.
func (t *IncrementalTarget) WriteAt(ctx context.Context, result *view.Result, refreshedAt time.Time) error {
	if t == nil || t.target == nil {
		return fmt.Errorf("%w: reporting target is required", ErrUnsupportedDestination)
	}
	if err := t.target.writeAt(ctx, result, refreshedAt); err != nil {
		return err
	}
	if t.cursor == nil {
		return nil
	}
	return t.cursor.UpsertCursor(ctx, store.Cursor{
		Name:      cursorName(result.ViewName, result.Version),
		Position:  refreshedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: refreshedAt.UTC(),
	})
}

func (t *IncrementalTarget) writeAt(ctx context.Context, result *view.Result, refreshedAt time.Time) error {
	return t.WriteAt(ctx, result, refreshedAt)
}

func cursorName(viewName, version string) string {
	if version == "" {
		version = "1.0.0"
	}
	return cursorPrefix + viewName + "." + version
}
