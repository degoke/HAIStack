package view_test

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/analytics"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/view"
)

type memWatermark struct {
	advanced bool
}

func (m *memWatermark) Since(context.Context, string, string) (time.Time, error) {
	return time.Time{}, nil
}

func (m *memWatermark) Advance(context.Context, string, string, time.Time) error {
	m.advanced = true
	return nil
}

func TestExportServiceAdvancesWatermarkAfterAllViewsSucceed(t *testing.T) {
	ctx := context.Background()
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), engine); err != nil {
		t.Fatalf("register: %v", err)
	}
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t))
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    engine,
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("executor: %v", err)
	}
	wm := &memWatermark{}
	svc, err := view.NewExportService(view.ExportServiceConfig{
		Jobs:      view.NewInMemoryViewExportJobStore(),
		Files:     view.NewInMemoryExportFileStore(),
		Executor:  exec,
		Watermark: wm,
	})
	if err != nil {
		t.Fatalf("NewExportService: %v", err)
	}
	job, err := svc.Kickoff(ctx, view.ViewExportRequest{
		Views: []view.ViewExportTarget{{ViewName: "patient_summary_view", Version: "1.0.0"}},
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.Status != view.ExportComplete {
		t.Fatalf("status=%q err=%q", job.Status, job.LastError)
	}
	if !wm.advanced {
		t.Fatal("expected watermark advance after successful export")
	}
	data, _, err := svc.GetFile(ctx, job.ID, job.Files[0].Filename)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected export artifact bytes")
	}
}

func TestExportServiceDoesNotAdvanceWatermarkOnFailure(t *testing.T) {
	ctx := context.Background()
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), engine); err != nil {
		t.Fatalf("register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: newMemResourceStore(),
		Engine:    engine,
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("executor: %v", err)
	}
	wm := &memWatermark{}
	svc, err := view.NewExportService(view.ExportServiceConfig{
		Jobs:      view.NewInMemoryViewExportJobStore(),
		Files:     view.NewInMemoryExportFileStore(),
		Executor:  exec,
		Watermark: wm,
	})
	if err != nil {
		t.Fatalf("NewExportService: %v", err)
	}
	job, err := svc.Kickoff(ctx, view.ViewExportRequest{
		Views: []view.ViewExportTarget{{ViewName: "missing_view", Version: "1.0.0"}},
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.Status != view.ExportError {
		t.Fatalf("status=%q, want error", job.Status)
	}
	if wm.advanced {
		t.Fatal("watermark must not advance when export fails")
	}
}

func TestWatermarkStoreImplementsAdvancer(t *testing.T) {
	var _ view.WatermarkAdvancer = (*analytics.WatermarkStore)(nil)
}

func TestExportFileStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := view.NewInMemoryExportFileStore()
	if err := store.Put(ctx, "job-1", "out.ndjson", []byte("row"), "application/fhir+ndjson"); err != nil {
		t.Fatal(err)
	}
	data, ct, err := store.Get(ctx, "job-1", "out.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "row" || ct != "application/fhir+ndjson" {
		t.Fatalf("data=%q ct=%q", data, ct)
	}
}

func TestExportServiceRollsBackPartialFilesOnFailure(t *testing.T) {
	ctx := context.Background()
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), engine); err != nil {
		t.Fatalf("register: %v", err)
	}
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t))
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    engine,
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("executor: %v", err)
	}
	files := view.NewInMemoryExportFileStore()
	svc, err := view.NewExportService(view.ExportServiceConfig{
		Jobs:     view.NewInMemoryViewExportJobStore(),
		Files:    files,
		Executor: exec,
	})
	if err != nil {
		t.Fatalf("NewExportService: %v", err)
	}
	job, err := svc.Kickoff(ctx, view.ViewExportRequest{
		Views: []view.ViewExportTarget{
			{ViewName: "patient_summary_view", Version: "1.0.0"},
			{ViewName: "missing_view", Version: "1.0.0"},
		},
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.Status != view.ExportError {
		t.Fatalf("status=%q, want error", job.Status)
	}
	if _, _, err := files.Get(ctx, job.ID, "patient_summary_view-1.0.0.ndjson"); err == nil {
		t.Fatal("expected partial export artifacts to be rolled back")
	}
}

func TestLocalExportFileStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := view.NewLocalExportFileStore(dir)
	if err != nil {
		t.Fatalf("NewLocalExportFileStore: %v", err)
	}
	if err := store.Put(ctx, "job-1", "out.ndjson", []byte("row"), "application/fhir+ndjson"); err != nil {
		t.Fatal(err)
	}
	data, ct, err := store.Get(ctx, "job-1", "out.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "row" || ct != "application/fhir+ndjson" {
		t.Fatalf("data=%q ct=%q", data, ct)
	}
	if err := store.DeleteJob(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Get(ctx, "job-1", "out.ndjson"); err == nil {
		t.Fatal("expected deleted export file")
	}
}
