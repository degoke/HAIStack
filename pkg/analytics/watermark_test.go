package analytics

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestWatermarkStoreMigratesLegacyViewCursor(t *testing.T) {
	ctx := context.Background()
	legacyAt := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	cursors := &memCursorStore{byName: map[string]store.Cursor{
		legacyCursorName("patient_summary_view", "1.0.0"): {
			Name:      legacyCursorName("patient_summary_view", "1.0.0"),
			Position:  legacyAt.Format(time.RFC3339Nano),
			UpdatedAt: legacyAt,
		},
	}}
	watermarks := NewWatermarkStore(cursors)

	since, err := watermarks.Since(ctx, "patient_summary_view", "1.0.0")
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if !since.Equal(legacyAt) {
		t.Fatalf("since = %v, want %v", since, legacyAt)
	}

	migrated, err := cursors.GetCursor(ctx, watermarkName("patient_summary_view", "1.0.0"))
	if err != nil {
		t.Fatalf("GetCursor migrated: %v", err)
	}
	if migrated == nil || migrated.Position == "" {
		t.Fatal("expected migrated watermark cursor")
	}
}
