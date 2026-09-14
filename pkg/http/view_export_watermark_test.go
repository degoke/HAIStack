package http_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/testkit/fixtures"
	"github.com/degoke/health-ai-stack/pkg/testkit/storetest"
	"github.com/degoke/health-ai-stack/pkg/view"
	"github.com/parquet-go/parquet-go"
)

type httpRecordWatermark struct {
	since    time.Time
	advanced time.Time
}

func (m *httpRecordWatermark) Since(context.Context, string, string) (time.Time, error) {
	return m.since.UTC(), nil
}

func (m *httpRecordWatermark) Advance(_ context.Context, _, _ string, at time.Time) error {
	m.advanced = at.UTC()
	return nil
}

func newHTTPExportServiceWithWatermark(t *testing.T) (http.Handler, *httpRecordWatermark) {
	t.Helper()
	cutoff := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	janeUpdated := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	johnUpdated := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	jane := fixtures.PatientJane(t)
	jane.LastUpdated = janeUpdated
	john := fixtures.PatientJohn(t)
	john.LastUpdated = johnUpdated
	resources := storetest.NewResourceStore()
	ctx := context.Background()
	if err := resources.Seed(ctx, jane, john); err != nil {
		t.Fatalf("seed resources: %v", err)
	}
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), engine); err != nil {
		t.Fatalf("register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    engine,
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	wm := &httpRecordWatermark{since: cutoff}
	svc, err := view.NewExportService(view.ExportServiceConfig{
		BasePath:  "/fhir",
		Jobs:      view.NewInMemoryViewExportJobStore(),
		Files:     view.NewInMemoryExportFileStore(),
		Executor:  exec,
		Watermark: wm,
	})
	if err != nil {
		t.Fatalf("NewExportService: %v", err)
	}
	return viewOpsHandler(t, hahttp.Config{ViewExportService: svc}), wm
}

func TestViewDefinitionExportHTTPAdvancesWatermarkFlatParquet(t *testing.T) {
	h, wm := newHTTPExportServiceWithWatermark(t)
	body := []byte(`{"resourceType":"Parameters","parameter":[
		{"name":"view","part":[
			{"name":"viewName","valueString":{"valueString":"patient_summary_view"}},
			{"name":"version","valueString":{"valueString":"1.0.0"}}
		]},
		{"name":"format","valueString":{"valueString":"parquet"}}
	]}`)
	rec := doRequestWithHeaders(t, h, http.MethodPost,
		"/fhir/$viewdefinition-export",
		body, map[string]string{"Prefer": "respond-async"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("kickoff status=%d body=%s", rec.Code, rec.Body.String())
	}
	statusURL := rec.Header().Get("Content-Location")
	if statusURL == "" {
		t.Fatal("missing Content-Location")
	}
	rec = doRequest(t, h, http.MethodGet, statusURL, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status poll=%d body=%s", rec.Code, rec.Body.String())
	}
	var status struct {
		Status string `json:"status"`
		Files  []struct {
			Filename string `json:"filename"`
			RowCount int    `json:"rowCount"`
			URL      string `json:"url"`
		} `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Status != string(view.ExportComplete) {
		t.Fatalf("status=%q, want complete", status.Status)
	}
	if len(status.Files) != 1 {
		t.Fatalf("files=%+v", status.Files)
	}
	if status.Files[0].RowCount != 1 {
		t.Fatalf("rowCount=%d, want 1 after since filter", status.Files[0].RowCount)
	}
	rec = doRequest(t, h, http.MethodGet, status.Files[0].URL, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("file download status=%d body=%s", rec.Code, rec.Body.String())
	}
	data := rec.Body.Bytes()
	if !view.IsParquetFile(data) {
		t.Fatal("expected parquet artifact")
	}
	ids := flatParquetPatientIDs(t, data)
	if len(ids) != 1 || ids[0] != "pat-jane" {
		t.Fatalf("parquet ids=%v, want [pat-jane]", ids)
	}
	want := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	if !wm.advanced.Equal(want) {
		t.Fatalf("watermark advanced=%v, want %v", wm.advanced, want)
	}
}

func flatParquetPatientIDs(t *testing.T, data []byte) []string {
	t.Helper()
	type patientRow struct {
		ID string `parquet:"id"`
	}
	rows, err := parquet.Read[patientRow](parquetBytesReader(data), int64(len(data)))
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

type parquetBytesReader []byte

func (b parquetBytesReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	return copy(p, b[off:]), nil
}
