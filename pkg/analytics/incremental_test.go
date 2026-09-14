package analytics_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/analytics"
	"github.com/degoke/health-ai-stack/pkg/store"
)

type memCursorStore struct {
	byName map[string]store.Cursor
}

func (m *memCursorStore) GetCursor(_ context.Context, name string) (*store.Cursor, error) {
	if c, ok := m.byName[name]; ok {
		copy := c
		return &copy, nil
	}
	return nil, nil
}

func (m *memCursorStore) UpsertCursor(_ context.Context, cursor store.Cursor) error {
	if m.byName == nil {
		m.byName = make(map[string]store.Cursor)
	}
	m.byName[cursor.Name] = cursor
	return nil
}

func (m *memCursorStore) DeleteCursor(_ context.Context, name string) error {
	delete(m.byName, name)
	return nil
}

func TestIncrementalTargetUsesWatermarkAfterRefreshHandler(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t))
	reporting := newMemReportingTableStore()
	cursors := &memCursorStore{}
	watermarks := analytics.NewWatermarkStore(cursors)
	target := analytics.NewIncrementalTarget(analytics.NewReportingTarget(reporting), watermarks)

	refreshAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	runnerWithClock, err := analytics.NewRunner(analytics.Config{
		Executor: newTestExecutor(t, resources),
		Now:      func() time.Time { return refreshAt },
	})
	if err != nil {
		t.Fatalf("NewRunner clock: %v", err)
	}

	payload, _ := json.Marshal(analytics.RefreshPayload{
		ViewName: analytics.ViewPatientSummary,
		Version:  "1.0.0",
	})
	handler := analytics.RefreshHandler(runnerWithClock, target, watermarks)
	if err := handler.HandleJob(ctx, store.JobRecord{Payload: payload}); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	since, err := watermarks.Since(ctx, analytics.ViewPatientSummary, "1.0.0")
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if !since.Equal(refreshAt) {
		t.Fatalf("watermark = %v, want %v", since, refreshAt)
	}
	sinceViaTarget, err := target.Since(ctx, analytics.ViewPatientSummary, "1.0.0")
	if err != nil {
		t.Fatalf("target Since: %v", err)
	}
	if !sinceViaTarget.Equal(refreshAt) {
		t.Fatalf("target Since = %v, want %v", sinceViaTarget, refreshAt)
	}
}
