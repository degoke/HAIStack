package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/proto"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/view"
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

type exportMemResourceStore struct {
	mu   sync.Mutex
	data map[string]*types.ResourceEnvelope
}

func newExportMemResourceStore() *exportMemResourceStore {
	return &exportMemResourceStore{data: make(map[string]*types.ResourceEnvelope)}
}

func exportResourceKey(resourceType, id string) string {
	return resourceType + "/" + id
}

func (s *exportMemResourceStore) Create(_ context.Context, res *types.ResourceEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := exportResourceKey(res.ResourceType, res.ID)
	if _, ok := s.data[key]; ok {
		return fmt.Errorf("resource already exists: %s", key)
	}
	s.data[key] = res
	return nil
}

func (s *exportMemResourceStore) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, ok := s.data[exportResourceKey(resourceType, id)]
	if !ok {
		return nil, fmt.Errorf("resource not found: %s/%s", resourceType, id)
	}
	return res, nil
}

func (s *exportMemResourceStore) Update(_ context.Context, res *types.ResourceEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := exportResourceKey(res.ResourceType, res.ID)
	if _, ok := s.data[key]; !ok {
		return fmt.Errorf("resource not found: %s", key)
	}
	s.data[key] = res
	return nil
}

func (s *exportMemResourceStore) Delete(_ context.Context, resourceType, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := exportResourceKey(resourceType, id)
	if _, ok := s.data[key]; !ok {
		return fmt.Errorf("resource not found: %s", key)
	}
	delete(s.data, key)
	return nil
}

func (s *exportMemResourceStore) Exists(_ context.Context, resourceType, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[exportResourceKey(resourceType, id)]
	return ok, nil
}

func (s *exportMemResourceStore) ListIDs(_ context.Context, resourceType string, limit, offset int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for key := range s.data {
		if !strings.HasPrefix(key, resourceType+"/") {
			continue
		}
		ids = append(ids, strings.TrimPrefix(key, resourceType+"/"))
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] < ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	if offset >= len(ids) {
		return nil, nil
	}
	end := len(ids)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return ids[offset:end], nil
}

func exportPatientEnvelope(t *testing.T, id, family string, lastUpdated time.Time) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType": "Patient",
		"id": "` + id + `",
		"gender": "female",
		"name": [{"given": ["Jane"], "family": "` + family + `"}],
		"telecom": [{"system": "phone", "value": "555-0100"}]
	}`)
	codec := proto.NewGoogleR4Codec()
	pb, err := codec.ParseJSONToProto("Patient", data)
	if err != nil {
		t.Fatalf("ParseJSONToProto: %v", err)
	}
	env, err := codec.ProtoToEnvelope("Patient", pb)
	if err != nil {
		t.Fatalf("ProtoToEnvelope: %v", err)
	}
	env.LastUpdated = lastUpdated
	return env
}

func newHTTPExportServiceWithWatermark(t *testing.T) (http.Handler, *httpRecordWatermark) {
	t.Helper()
	cutoff := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	janeUpdated := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	johnUpdated := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	resources := newExportMemResourceStore()
	ctx := context.Background()
	if err := resources.Create(ctx, exportPatientEnvelope(t, "pat-jane", "Doe", janeUpdated)); err != nil {
		t.Fatalf("seed jane: %v", err)
	}
	if err := resources.Create(ctx, exportPatientEnvelope(t, "pat-john", "Smith", johnUpdated)); err != nil {
		t.Fatalf("seed john: %v", err)
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
	if !view.IsParquetFile(rec.Body.Bytes()) {
		t.Fatal("expected parquet artifact")
	}
	rows, err := view.ParquetFileRowCount(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("ParquetFileRowCount: %v", err)
	}
	if rows != 1 {
		t.Fatalf("parquet rows=%d, want 1", rows)
	}
	want := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	if !wm.advanced.Equal(want) {
		t.Fatalf("watermark advanced=%v, want %v", wm.advanced, want)
	}
}
