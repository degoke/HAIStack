package analytics_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/analytics"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/view"
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
	})
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
