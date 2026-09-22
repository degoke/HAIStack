package view_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/analytics"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/testkit/viewtest"
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

type recordWatermark struct {
	since    time.Time
	advanced time.Time
}

func (m *recordWatermark) Since(context.Context, string, string) (time.Time, error) {
	return m.since.UTC(), nil
}

func (m *recordWatermark) Advance(_ context.Context, _, _ string, at time.Time) error {
	m.advanced = at.UTC()
	return nil
}

func TestExportServiceDefaultsToNDJSONFormat(t *testing.T) {
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
	svc, err := view.NewExportService(view.ExportServiceConfig{
		Jobs:     view.NewInMemoryViewExportJobStore(),
		Files:    view.NewInMemoryExportFileStore(),
		Executor: exec,
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
	if job.Request.Format != view.FormatNDJSON {
		t.Fatalf("request format=%q, want ndjson default", job.Request.Format)
	}
	if len(job.Files) != 1 || job.Files[0].Filename != "patient_summary_view-1.0.0.ndjson" {
		t.Fatalf("files=%v", job.Files)
	}
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

func TestExportServiceWritesParquetBinary(t *testing.T) {
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
	svc, err := view.NewExportService(view.ExportServiceConfig{
		Jobs:     view.NewInMemoryViewExportJobStore(),
		Files:    view.NewInMemoryExportFileStore(),
		Executor: exec,
	})
	if err != nil {
		t.Fatalf("NewExportService: %v", err)
	}
	job, err := svc.Kickoff(ctx, view.ViewExportRequest{
		Format: view.FormatParquet,
		Views:  []view.ViewExportTarget{{ViewName: "patient_summary_view", Version: "1.0.0"}},
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.Status != view.ExportComplete {
		t.Fatalf("status=%q err=%q", job.Status, job.LastError)
	}
	data, contentType, err := svc.GetFile(ctx, job.ID, job.Files[0].Filename)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if contentType != view.ParquetContentType {
		t.Fatalf("contentType=%q", contentType)
	}
	if !view.IsParquetFile(data) {
		t.Fatal("expected parquet binary artifact")
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

func TestExportServiceParquetFHIRRecordsLayoutMetadata(t *testing.T) {
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
	resources.Seed(t, patientJane(t), patientJohn(t))
	exec, err := view.NewExecutor(view.Config{
		Resources:      resources,
		Engine:         engine,
		Registry:       reg,
		ProfileCatalog: bundledPatientCatalog(t),
	})
	if err != nil {
		t.Fatalf("executor: %v", err)
	}
	svc, err := view.NewExportService(view.ExportServiceConfig{
		Jobs:     view.NewInMemoryViewExportJobStore(),
		Files:    view.NewInMemoryExportFileStore(),
		Executor: exec,
	})
	if err != nil {
		t.Fatalf("NewExportService: %v", err)
	}
	job, err := svc.Kickoff(ctx, view.ViewExportRequest{
		Views:         []view.ViewExportTarget{{ViewName: "patient_summary_view", Version: "1.0.0"}},
		Format:        view.FormatParquet,
		ParquetLayout: view.ParquetLayoutFHIR,
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.Status != view.ExportComplete {
		t.Fatalf("status=%q err=%q", job.Status, job.LastError)
	}
	if len(job.Files) != 1 {
		t.Fatalf("files=%v", job.Files)
	}
	if job.Files[0].ParquetLayout != view.ParquetLayoutFHIR {
		t.Fatalf("parquetLayout=%q", job.Files[0].ParquetLayout)
	}
	data, _, err := svc.GetFile(ctx, job.ID, job.Files[0].Filename)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if !view.IsParquetFile(data) {
		t.Fatal("expected parquet artifact")
	}
}

func TestExportServiceAdvancesWatermarkToMaxLastUpdatedFlatParquet(t *testing.T) {
	ctx := context.Background()
	cutoff := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	patients := viewtest.IncrementalPatientSummaryPatients(t)
	exec := viewtest.NewPatientSummaryExecutor(t, patients)
	wm := &recordWatermark{since: cutoff}
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
		Format: view.FormatParquet,
		Views:  []view.ViewExportTarget{{ViewName: "patient_summary_view", Version: "1.0.0"}},
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.Status != view.ExportComplete {
		t.Fatalf("status=%q err=%q", job.Status, job.LastError)
	}
	if len(job.Files) != 1 {
		t.Fatalf("files=%v", job.Files)
	}
	if job.Files[0].RowCount != 1 {
		t.Fatalf("rowCount=%d, want 1 after since filter", job.Files[0].RowCount)
	}
	data, _, err := svc.GetFile(ctx, job.ID, job.Files[0].Filename)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	rows, err := view.ParquetFileRowCount(data)
	if err != nil {
		t.Fatalf("ParquetFileRowCount: %v", err)
	}
	if rows != 1 {
		t.Fatalf("parquet rows=%d, want 1", rows)
	}
	if !wm.advanced.Equal(patients.Jane.LastUpdated.UTC()) {
		t.Fatalf("watermark advanced=%v, want %v", wm.advanced, patients.Jane.LastUpdated.UTC())
	}
}

func TestExportServiceStreamsParquetWithoutFullFilePut(t *testing.T) {
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
	inner, err := view.NewLocalExportFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalExportFileStore: %v", err)
	}
	files := &streamOnlyExportFiles{inner: inner}
	svc, err := view.NewExportService(view.ExportServiceConfig{
		Jobs:     view.NewInMemoryViewExportJobStore(),
		Files:    files,
		Executor: exec,
	})
	if err != nil {
		t.Fatalf("NewExportService: %v", err)
	}
	job, err := svc.Kickoff(ctx, view.ViewExportRequest{
		Format: view.FormatParquet,
		Views:  []view.ViewExportTarget{{ViewName: "patient_summary_view", Version: "1.0.0"}},
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.Status != view.ExportComplete {
		t.Fatalf("status=%q err=%q", job.Status, job.LastError)
	}
	if files.putCalls != 0 {
		t.Fatalf("putCalls=%d, want 0 (must stream via PutStream, not os.ReadFile + Put)", files.putCalls)
	}
	if files.putStreamCalls != 1 {
		t.Fatalf("putStreamCalls=%d, want 1", files.putStreamCalls)
	}
	data, contentType, err := svc.GetFile(ctx, job.ID, job.Files[0].Filename)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if contentType != view.ParquetContentType {
		t.Fatalf("contentType=%q", contentType)
	}
	if !view.IsParquetFile(data) {
		t.Fatal("expected parquet binary artifact")
	}
}

func TestLocalExportFileStorePutStreamDoesNotUseFullFileReadBuffer(t *testing.T) {
	ctx := context.Background()
	store, err := view.NewLocalExportFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalExportFileStore: %v", err)
	}
	payload := bytes.Repeat([]byte("e"), 256*1024)
	probe := &readSizeProbe{r: bytes.NewReader(payload)}
	if err := store.PutStream(ctx, "job-1", "big.bin", probe, int64(len(payload)), "application/octet-stream"); err != nil {
		t.Fatalf("PutStream: %v", err)
	}
	if probe.maxRead >= len(payload) {
		t.Fatalf("max Read dest %d equals full payload; expected io.Copy buffer", probe.maxRead)
	}
	got, ct, err := store.Get(ctx, "job-1", "big.bin")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ct != "application/octet-stream" || !bytes.Equal(got, payload) {
		t.Fatalf("got %d bytes ct=%q", len(got), ct)
	}
}

type readSizeProbe struct {
	r       io.Reader
	maxRead int
}

func (p *readSizeProbe) Read(b []byte) (int, error) {
	if len(b) > p.maxRead {
		p.maxRead = len(b)
	}
	return p.r.Read(b)
}

type streamOnlyExportFiles struct {
	inner          view.ExportFileStoreWithStream
	mu             sync.Mutex
	putCalls       int
	putStreamCalls int
}

func (s *streamOnlyExportFiles) Put(ctx context.Context, jobID, filename string, data []byte, contentType string) error {
	s.mu.Lock()
	s.putCalls++
	s.mu.Unlock()
	if len(data) > 0 {
		return fmt.Errorf("buffered Put of %d bytes is not allowed", len(data))
	}
	return s.inner.Put(ctx, jobID, filename, data, contentType)
}

func (s *streamOnlyExportFiles) PutStream(ctx context.Context, jobID, filename string, r io.Reader, size int64, contentType string) error {
	s.mu.Lock()
	s.putStreamCalls++
	s.mu.Unlock()
	return s.inner.PutStream(ctx, jobID, filename, r, size, contentType)
}

func (s *streamOnlyExportFiles) Get(ctx context.Context, jobID, filename string) ([]byte, string, error) {
	return s.inner.Get(ctx, jobID, filename)
}

func (s *streamOnlyExportFiles) Open(ctx context.Context, jobID, filename string) (io.ReadCloser, string, error) {
	return s.inner.Open(ctx, jobID, filename)
}

func (s *streamOnlyExportFiles) DeleteJob(ctx context.Context, jobID string) error {
	return s.inner.DeleteJob(ctx, jobID)
}

var _ view.ExportFileStoreWithStream = (*streamOnlyExportFiles)(nil)
