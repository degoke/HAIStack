package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/view"
)

const watermarkPrefix = "analytics.watermark."
const legacyCursorPrefix = "analytics.view."

// WatermarkStore tracks SQL-on-FHIR change-detection watermarks per view.
type WatermarkStore struct {
	cursor store.CursorStore
	now    func() time.Time
}

// NewWatermarkStore returns a watermark store backed by CursorStore.
func NewWatermarkStore(cursor store.CursorStore) *WatermarkStore {
	return &WatermarkStore{
		cursor: cursor,
		now:    time.Now,
	}
}

// Since returns the last successful watermark timestamp for a view, if any.
// When no watermark exists, a legacy analytics.view.* cursor is migrated forward once.
func (w *WatermarkStore) Since(ctx context.Context, viewName, version string) (time.Time, error) {
	if w == nil || w.cursor == nil {
		return time.Time{}, nil
	}
	since, err := w.readCursorTime(ctx, watermarkName(viewName, version))
	if err != nil || !since.IsZero() {
		return since, err
	}
	return w.migrateLegacyCursor(ctx, viewName, version)
}

func (w *WatermarkStore) readCursorTime(ctx context.Context, name string) (time.Time, error) {
	record, err := w.cursor.GetCursor(ctx, name)
	if err != nil || record == nil || record.Position == "" {
		return time.Time{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, record.Position)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse analytics watermark: %w", err)
	}
	return parsed.UTC(), nil
}

func (w *WatermarkStore) migrateLegacyCursor(ctx context.Context, viewName, version string) (time.Time, error) {
	legacy, err := w.readCursorTime(ctx, legacyCursorName(viewName, version))
	if err != nil || legacy.IsZero() {
		return legacy, err
	}
	if err := w.Advance(ctx, viewName, version, legacy); err != nil {
		return time.Time{}, err
	}
	return legacy, nil
}

// Advance stores a new watermark after a successful incremental run.
func (w *WatermarkStore) Advance(ctx context.Context, viewName, version string, refreshedAt time.Time) error {
	if w == nil || w.cursor == nil {
		return nil
	}
	if refreshedAt.IsZero() {
		refreshedAt = w.now().UTC()
	}
	return w.cursor.UpsertCursor(ctx, store.Cursor{
		Name:      watermarkName(viewName, version),
		Position:  refreshedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: refreshedAt.UTC(),
	})
}

func watermarkName(viewName, version string) string {
	if version == "" {
		version = "1.0.0"
	}
	return watermarkPrefix + viewName + "." + version
}

func legacyCursorName(viewName, version string) string {
	if version == "" {
		version = "1.0.0"
	}
	return legacyCursorPrefix + viewName + "." + version
}

var _ view.WatermarkAdvancer = (*WatermarkStore)(nil)
