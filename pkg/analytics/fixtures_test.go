package analytics_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/proto"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/view"
)

var (
	_ store.ResourceStore       = (*memResourceStore)(nil)
	_ store.ReportingTableStore = (*memReportingTableStore)(nil)
)

type memResourceStore struct {
	mu   sync.Mutex
	data map[string]*types.ResourceEnvelope
}

func newMemResourceStore() *memResourceStore {
	return &memResourceStore{data: make(map[string]*types.ResourceEnvelope)}
}

func resourceKey(resourceType, id string) string {
	return resourceType + "/" + id
}

func (s *memResourceStore) Create(_ context.Context, res *types.ResourceEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceKey(res.ResourceType, res.ID)
	if _, ok := s.data[key]; ok {
		return fmt.Errorf("resource already exists: %s", key)
	}
	s.data[key] = res
	return nil
}

func (s *memResourceStore) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, ok := s.data[resourceKey(resourceType, id)]
	if !ok {
		return nil, fmt.Errorf("resource not found: %s/%s", resourceType, id)
	}
	return res, nil
}

func (s *memResourceStore) Update(_ context.Context, res *types.ResourceEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceKey(res.ResourceType, res.ID)
	if _, ok := s.data[key]; !ok {
		return fmt.Errorf("resource not found: %s", key)
	}
	s.data[key] = res
	return nil
}

func (s *memResourceStore) Delete(_ context.Context, resourceType, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceKey(resourceType, id)
	if _, ok := s.data[key]; !ok {
		return fmt.Errorf("resource not found: %s", key)
	}
	delete(s.data, key)
	return nil
}

func (s *memResourceStore) Exists(_ context.Context, resourceType, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[resourceKey(resourceType, id)]
	return ok, nil
}

func (s *memResourceStore) ListIDs(_ context.Context, resourceType string, limit, offset int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for _, res := range s.data {
		if res.ResourceType != resourceType {
			continue
		}
		ids = append(ids, res.ID)
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

func (s *memResourceStore) Seed(t *testing.T, resources ...*types.ResourceEnvelope) {
	t.Helper()
	ctx := context.Background()
	for _, res := range resources {
		if err := s.Create(ctx, res); err != nil {
			t.Fatalf("seed %s/%s: %v", res.ResourceType, res.ID, err)
		}
	}
}

type memReportingTableStore struct {
	mu   sync.Mutex
	meta map[string]store.ReportingTableMeta
	rows map[string][]map[string]any
}

func newMemReportingTableStore() *memReportingTableStore {
	return &memReportingTableStore{
		meta: make(map[string]store.ReportingTableMeta),
		rows: make(map[string][]map[string]any),
	}
}

func reportingKey(viewName, version string) string {
	return viewName + "|" + version
}

func (s *memReportingTableStore) Refresh(_ context.Context, meta store.ReportingTableMeta, rows []map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := reportingKey(meta.ViewName, meta.ViewVersion)
	s.meta[key] = meta
	copied := make([]map[string]any, len(rows))
	for i, row := range rows {
		copied[i] = make(map[string]any, len(row))
		for k, v := range row {
			copied[i][k] = v
		}
	}
	s.rows[key] = copied
	return nil
}

func (s *memReportingTableStore) QueryRows(_ context.Context, viewName, viewVersion string) ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.rows[reportingKey(viewName, viewVersion)]
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		out[i] = make(map[string]any, len(row))
		for k, v := range row {
			out[i][k] = v
		}
	}
	return out, nil
}

func (s *memReportingTableStore) GetMeta(_ context.Context, viewName, viewVersion string) (*store.ReportingTableMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, ok := s.meta[reportingKey(viewName, viewVersion)]
	if !ok {
		return nil, fmt.Errorf("reporting table not found: %s/%s", viewName, viewVersion)
	}
	copyMeta := meta
	return &copyMeta, nil
}

func defaultEngine(t *testing.T) fhirpath.Engine {
	t.Helper()
	eng, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return eng
}

func newTestExecutor(t *testing.T, store *memResourceStore) *view.Executor {
	t.Helper()
	reg := view.NewRegistry()
	engine := defaultEngine(t)
	for _, builtin := range []func() []byte{
		view.PatientSummaryView,
		view.AppointmentView,
		view.ObservationView,
	} {
		if _, err := reg.Register(builtin(), engine); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: store,
		Engine:    engine,
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	return exec
}

func envelopeFromJSON(t *testing.T, resourceType string, data []byte) *types.ResourceEnvelope {
	t.Helper()
	codec := proto.NewGoogleR4Codec()
	pb, err := codec.ParseJSONToProto(resourceType, data)
	if err != nil {
		t.Fatalf("ParseJSONToProto %s: %v", resourceType, err)
	}
	env, err := codec.ProtoToEnvelope(resourceType, pb)
	if err != nil {
		t.Fatalf("ProtoToEnvelope %s: %v", resourceType, err)
	}
	return env
}

func patientJane(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType": "Patient",
		"id": "pat-jane",
		"gender": "female",
		"name": [{"given": ["Jane"], "family": "Doe"}],
		"telecom": [{"system": "phone", "value": "555-0100"}]
	}`)
	return envelopeFromJSON(t, "Patient", data)
}

func patientJohn(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType": "Patient",
		"id": "pat-john",
		"gender": "male",
		"name": [{"given": ["John"], "family": "Smith"}]
	}`)
	return envelopeFromJSON(t, "Patient", data)
}

func fixedNow() func() time.Time {
	ts := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return ts }
}
