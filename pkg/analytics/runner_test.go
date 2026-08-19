package analytics_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/analytics"
	"github.com/degoke/health-ai-stack/pkg/view"
)

func TestRunner_RefreshWritesReportingRows(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t), patientJohn(t))
	reporting := newMemReportingTableStore()

	runner, err := analytics.NewRunner(analytics.Config{Executor: newTestExecutor(t, resources)})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	result, err := runner.Run(ctx, analytics.RunRequest{
		ViewName: analytics.ViewPatientSummary,
		Mode:     analytics.ModeRefresh,
		Destination: analytics.Destination{
			Reporting: analytics.NewReportingTarget(reporting),
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.RowCount != 2 {
		t.Fatalf("RowCount = %d, want 2", result.RowCount)
	}

	rows, err := reporting.QueryRows(ctx, analytics.ViewPatientSummary, "1.0.0")
	if err != nil {
		t.Fatalf("QueryRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
}

func TestRunner_UsesConfiguredClockForResultAndRefreshMetadata(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	reporting := newMemReportingTableStore()
	runner, err := analytics.NewRunner(analytics.Config{
		Executor: newTestExecutor(t, newMemResourceStore()),
		Now:      now,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	result, err := runner.Run(ctx, analytics.RunRequest{
		ViewName: analytics.ViewPatientSummary,
		Mode:     analytics.ModeRefresh,
		Destination: analytics.Destination{
			Reporting: analytics.NewReportingTarget(reporting),
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Metadata.ExecutedAt.Equal(now()) {
		t.Fatalf("ExecutedAt = %v, want %v", result.Metadata.ExecutedAt, now())
	}

	meta, err := reporting.GetMeta(ctx, analytics.ViewPatientSummary, "1.0.0")
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if !meta.RefreshedAt.Equal(now()) {
		t.Fatalf("RefreshedAt = %v, want %v", meta.RefreshedAt, now())
	}
}

func TestRunner_ExportWritesCSV(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t))
	var buf bytes.Buffer

	runner, err := analytics.NewRunner(analytics.Config{Executor: newTestExecutor(t, resources)})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = runner.Run(ctx, analytics.RunRequest{
		ViewName: analytics.ViewPatientSummary,
		Mode:     analytics.ModeExport,
		Destination: analytics.Destination{
			Sink: analytics.NewCSVSink(&buf),
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "id,given,family,gender,phone\n") {
		t.Fatalf("csv header = %q", out)
	}
	if !strings.Contains(out, "pat-jane,Jane,Doe,female,555-0100") {
		t.Fatalf("csv row missing pat-jane: %q", out)
	}
}

func TestRunner_PropagatesViewExecutionFailure(t *testing.T) {
	ctx := context.Background()
	reg := view.NewRegistry()
	exec, err := view.NewExecutor(view.Config{
		Resources: newMemResourceStore(),
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	runner, err := analytics.NewRunner(analytics.Config{Executor: exec})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = runner.Run(ctx, analytics.RunRequest{
		ViewName: analytics.ViewPatientSummary,
		Mode:     analytics.ModeRefresh,
		Destination: analytics.Destination{
			Reporting: analytics.NewReportingTarget(newMemReportingTableStore()),
		},
	})
	if !errors.Is(err, view.ErrViewNotFound) {
		t.Fatalf("Run err = %v, want ErrViewNotFound", err)
	}
}

func TestRunner_UnsupportedView(t *testing.T) {
	ctx := context.Background()
	runner, err := analytics.NewRunner(analytics.Config{Executor: newTestExecutor(t, newMemResourceStore())})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = runner.Run(ctx, analytics.RunRequest{
		ViewName: "unknown_view",
		Mode:     analytics.ModeRefresh,
		Destination: analytics.Destination{
			Reporting: analytics.NewReportingTarget(newMemReportingTableStore()),
		},
	})
	if !errors.Is(err, analytics.ErrUnsupportedView) {
		t.Fatalf("Run err = %v, want ErrUnsupportedView", err)
	}
}

func TestRunner_UnsupportedDestination(t *testing.T) {
	ctx := context.Background()
	runner, err := analytics.NewRunner(analytics.Config{Executor: newTestExecutor(t, newMemResourceStore())})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = runner.Run(ctx, analytics.RunRequest{
		ViewName:    analytics.ViewPatientSummary,
		Mode:        analytics.ModeRefresh,
		Destination: analytics.Destination{},
	})
	if !errors.Is(err, analytics.ErrUnsupportedDestination) {
		t.Fatalf("Run err = %v, want ErrUnsupportedDestination", err)
	}

	_, err = runner.Run(ctx, analytics.RunRequest{
		ViewName:    analytics.ViewPatientSummary,
		Mode:        analytics.ModeExport,
		Destination: analytics.Destination{},
	})
	if !errors.Is(err, analytics.ErrUnsupportedDestination) {
		t.Fatalf("Run err = %v, want ErrUnsupportedDestination", err)
	}

	var buf bytes.Buffer
	_, err = runner.Run(ctx, analytics.RunRequest{
		ViewName: analytics.ViewPatientSummary,
		Mode:     analytics.ModeRefresh,
		Destination: analytics.Destination{
			Reporting: analytics.NewReportingTarget(newMemReportingTableStore()),
			Sink:      analytics.NewCSVSink(&buf),
		},
	})
	if !errors.Is(err, analytics.ErrUnsupportedDestination) {
		t.Fatalf("Run with both destinations err = %v, want ErrUnsupportedDestination", err)
	}
}

func TestRunner_RejectsCustomDefinitionUsingBuiltInName(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	reg := view.NewRegistry()
	engine := defaultEngine(t)
	custom := bytes.Replace(view.PatientSummaryView(), []byte(`"path": "Patient.gender"`), []byte(`"path": "Patient.id"`), 1)
	if _, err := reg.Register(custom, engine); err != nil {
		t.Fatalf("Register custom definition: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    engine,
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	runner, err := analytics.NewRunner(analytics.Config{Executor: exec})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = runner.Run(ctx, analytics.RunRequest{
		ViewName: analytics.ViewPatientSummary,
		Mode:     analytics.ModeRefresh,
		Destination: analytics.Destination{
			Reporting: analytics.NewReportingTarget(newMemReportingTableStore()),
		},
	})
	if !errors.Is(err, analytics.ErrUnsupportedView) {
		t.Fatalf("Run err = %v, want ErrUnsupportedView", err)
	}
}

func TestDeferredSinksNotImplemented(t *testing.T) {
	ctx := context.Background()
	result := &view.Result{ViewName: analytics.ViewPatientSummary, Version: "1.0.0"}
	sinks := []analytics.RowSink{
		analytics.NewParquetSink(),
		analytics.NewWarehouseSink(),
		analytics.NewLakehouseSink(),
		analytics.NewManifestExportSink(),
	}
	for _, sink := range sinks {
		if err := sink.WriteRows(ctx, result); !errors.Is(err, analytics.ErrSinkNotImplemented) {
			t.Fatalf("WriteRows err = %v, want ErrSinkNotImplemented", err)
		}
	}
}
