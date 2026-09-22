package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	hahttp "github.com/degoke/haistack/pkg/http"
	"github.com/degoke/haistack/pkg/testkit/parquettest"
	"github.com/degoke/haistack/pkg/testkit/viewtest"
	"github.com/degoke/haistack/pkg/view"
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
	exec := viewtest.NewPatientSummaryExecutor(t, viewtest.IncrementalPatientSummaryPatients(t))
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
	ids := parquettest.IDs(t, data)
	if len(ids) != 1 || ids[0] != "pat-jane" {
		t.Fatalf("parquet ids=%v, want [pat-jane]", ids)
	}
	want := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	if !wm.advanced.Equal(want) {
		t.Fatalf("watermark advanced=%v, want %v", wm.advanced, want)
	}
}
