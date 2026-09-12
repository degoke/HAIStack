package analytics_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/analytics"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/view"
	"github.com/parquet-go/parquet-go"
)

func TestRunnerSkipsFlatExecuteForFHIRParquetExport(t *testing.T) {
	exec := newFHIRExecutor(t)
	runner, err := analytics.NewRunner(analytics.Config{Executor: exec})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	var buf bytes.Buffer
	sink := analytics.NewParquetFileSinkWithConfig(analytics.ParquetFileSinkConfig{
		Writer:   &buf,
		Layout:   view.ParquetLayoutFHIR,
		Executor: exec,
	})
	result, err := runner.Run(context.Background(), analytics.RunRequest{
		ViewName: analytics.ViewPatientSummary,
		Version:  "1.0.0",
		Mode:     analytics.ModeExport,
		Destination: analytics.Destination{
			Sink: sink,
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.RowCount != 2 {
		t.Fatalf("rowCount=%d, want 2", result.RowCount)
	}
	if !view.IsParquetFile(buf.Bytes()) {
		t.Fatal("expected parquet output")
	}
}

func TestRunnerFHIRParquetExportRespectsSince(t *testing.T) {
	cutoff := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	jane := patientJane(t)
	jane.LastUpdated = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	john := patientJohn(t)
	john.LastUpdated = time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	resources := newMemResourceStore()
	resources.Seed(t, jane, john)
	exec := newFHIRExecutorWithStore(t, resources)

	runner, err := analytics.NewRunner(analytics.Config{Executor: exec})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	var buf bytes.Buffer
	sink := analytics.NewParquetFileSinkWithConfig(analytics.ParquetFileSinkConfig{
		Writer:   &buf,
		Layout:   view.ParquetLayoutFHIR,
		Executor: exec,
	})
	result, err := runner.Run(context.Background(), analytics.RunRequest{
		ViewName: analytics.ViewPatientSummary,
		Version:  "1.0.0",
		Mode:     analytics.ModeExport,
		Since:    cutoff,
		Destination: analytics.Destination{
			Sink: sink,
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.RowCount != 1 {
		t.Fatalf("rowCount=%d, want 1 after since filter", result.RowCount)
	}
	ids := parquetPatientIDs(t, buf.Bytes())
	if len(ids) != 1 || ids[0] != "pat-jane" {
		t.Fatalf("parquet ids=%v, want [pat-jane]", ids)
	}
}

func TestRunnerFHIRParquetExportPopulatesMetadata(t *testing.T) {
	exec := newFHIRExecutor(t)
	runner, err := analytics.NewRunner(analytics.Config{Executor: exec})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	var buf bytes.Buffer
	sink := analytics.NewParquetFileSinkWithConfig(analytics.ParquetFileSinkConfig{
		Writer:   &buf,
		Layout:   view.ParquetLayoutFHIR,
		Executor: exec,
	})
	result, err := runner.Run(context.Background(), analytics.RunRequest{
		ViewName: analytics.ViewPatientSummary,
		Version:  "1.0.0",
		Mode:     analytics.ModeExport,
		Destination: analytics.Destination{
			Sink: sink,
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Metadata.Scanned != 2 {
		t.Fatalf("scanned=%d, want 2", result.Metadata.Scanned)
	}
	if result.Metadata.Filtered != 2 {
		t.Fatalf("filtered=%d, want 2", result.Metadata.Filtered)
	}
	if result.Metadata.SourceResourceType != "Patient" {
		t.Fatalf("sourceResourceType=%q, want Patient", result.Metadata.SourceResourceType)
	}
}

func TestExportHandlerUsesWatermarkSince(t *testing.T) {
	cutoff := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	jane := patientJane(t)
	jane.LastUpdated = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	john := patientJohn(t)
	john.LastUpdated = time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	resources := newMemResourceStore()
	resources.Seed(t, jane, john)
	exec := newFHIRExecutorWithStore(t, resources)
	runner, err := analytics.NewRunner(analytics.Config{Executor: exec})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	watermarks := analytics.NewWatermarkStore(&testMemCursorStore{byName: make(map[string]store.Cursor)})
	if err := watermarks.Advance(context.Background(), analytics.ViewPatientSummary, "1.0.0", cutoff); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	var buf bytes.Buffer
	handler := analytics.ExportHandlerWithConfig(runner, analytics.ExportHandlerConfig{
		Writer:   &buf,
		Executor: exec,
	}, watermarks)
	payload, err := json.Marshal(analytics.ExportPayload{
		ViewName:      analytics.ViewPatientSummary,
		Version:       "1.0.0",
		Format:        analytics.FormatParquet,
		ParquetLayout: view.ParquetLayoutFHIR,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := handler.HandleJob(context.Background(), store.JobRecord{Payload: payload}); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}
	ids := parquetPatientIDs(t, buf.Bytes())
	if len(ids) != 1 || ids[0] != "pat-jane" {
		t.Fatalf("parquet ids=%v, want [pat-jane]", ids)
	}
}

func parquetPatientIDs(t *testing.T, data []byte) []string {
	t.Helper()
	type patientRow struct {
		ID string `parquet:"id"`
	}
	rows, err := parquet.Read[patientRow](bytesReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.ID != "" {
			ids = append(ids, row.ID)
		}
	}
	return ids
}

type bytesReader []byte

func (b bytesReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	return copy(p, b[off:]), nil
}

type testMemCursorStore struct {
	byName map[string]store.Cursor
}

func (m *testMemCursorStore) GetCursor(_ context.Context, name string) (*store.Cursor, error) {
	cursor, ok := m.byName[name]
	if !ok {
		return nil, nil
	}
	copy := cursor
	return &copy, nil
}

func (m *testMemCursorStore) UpsertCursor(_ context.Context, cursor store.Cursor) error {
	if m.byName == nil {
		m.byName = make(map[string]store.Cursor)
	}
	m.byName[cursor.Name] = cursor
	return nil
}

func (m *testMemCursorStore) DeleteCursor(_ context.Context, name string) error {
	delete(m.byName, name)
	return nil
}

func TestExportHandlerWithConfigWritesFHIRParquet(t *testing.T) {
	exec := newFHIRExecutor(t)
	runner, err := analytics.NewRunner(analytics.Config{Executor: exec})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	var buf bytes.Buffer
	handler := analytics.ExportHandlerWithConfig(runner, analytics.ExportHandlerConfig{
		Writer:   &buf,
		Executor: exec,
	}, nil)
	payload, err := json.Marshal(analytics.ExportPayload{
		ViewName:      analytics.ViewPatientSummary,
		Version:       "1.0.0",
		Format:        analytics.FormatParquet,
		ParquetLayout: view.ParquetLayoutFHIR,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := handler.HandleJob(context.Background(), store.JobRecord{Payload: payload}); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}
	if !view.IsParquetFile(buf.Bytes()) {
		t.Fatal("expected parquet output")
	}
}
