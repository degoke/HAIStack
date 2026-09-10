package analytics_test

import (
	"context"
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

func TestIncrementalTargetTracksCursor(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t))
	reporting := newMemReportingTableStore()
	cursors := &memCursorStore{}
	target := analytics.NewIncrementalTarget(analytics.NewReportingTarget(reporting), cursors)

	runner, err := analytics.NewRunner(analytics.Config{Executor: newTestExecutor(t, resources)})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	firstAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	_, err = runner.Run(ctx, analytics.RunRequest{
		ViewName: analytics.ViewPatientSummary,
		Mode:     analytics.ModeRefresh,
		Destination: analytics.Destination{
			Reporting: target,
		},
		Incremental: true,
	})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	since, err := target.Since(ctx, analytics.ViewPatientSummary, "1.0.0")
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if since.IsZero() {
		t.Fatal("expected cursor after refresh")
	}
	_ = firstAt
}
