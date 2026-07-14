package view_test

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
	_ store.ResourceStore = (*memResourceStore)(nil)
	_ view.Authorizer     = (*fakeAuthorizer)(nil)
	_ view.AuditLogger    = (*fakeAuditLogger)(nil)
)

// memResourceStore is a minimal in-memory store for view tests.
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
	for key, res := range s.data {
		if res.ResourceType != resourceType {
			continue
		}
		ids = append(ids, res.ID)
		_ = key
	}
	// Sort for deterministic pagination in tests.
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

// fakeAuthorizer denies views that require a permission not in the allowed set.
type fakeAuthorizer struct {
	mu      sync.Mutex
	allowed map[string]struct{}
	calls   []view.AuthRequest
}

func newFakeAuthorizer(allowed ...string) *fakeAuthorizer {
	m := make(map[string]struct{}, len(allowed))
	for _, p := range allowed {
		m[p] = struct{}{}
	}
	return &fakeAuthorizer{allowed: m}
}

func (a *fakeAuthorizer) AuthorizeView(_ context.Context, req view.AuthRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, req)
	for _, p := range req.Permissions {
		if _, ok := a.allowed[p]; ok {
			return nil
		}
	}
	return fmt.Errorf("missing permission")
}

func (a *fakeAuthorizer) Calls() []view.AuthRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]view.AuthRequest, len(a.calls))
	copy(out, a.calls)
	return out
}

// fakeAuditLogger records audit records in memory.
type fakeAuditLogger struct {
	mu      sync.Mutex
	records []view.AuditRecord
}

func (a *fakeAuditLogger) LogViewAccess(_ context.Context, rec view.AuditRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, rec)
	return nil
}

func (a *fakeAuditLogger) Records() []view.AuditRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]view.AuditRecord, len(a.records))
	copy(out, a.records)
	return out
}

// fixedClock returns a deterministic time and never advances.
type fixedClock struct {
	mu        sync.Mutex
	fixed     time.Time
	callCount int
}

func newFixedClock(t time.Time) *fixedClock {
	return &fixedClock{fixed: t}
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.callCount++
	return c.fixed
}

func defaultEngine(t *testing.T) fhirpath.Engine {
	t.Helper()
	eng, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return eng
}

func googleR4Codec(t *testing.T) *proto.GoogleR4Codec {
	t.Helper()
	return proto.NewGoogleR4Codec()
}

func envelopeFromJSON(t *testing.T, resourceType string, data []byte) *types.ResourceEnvelope {
	t.Helper()
	codec := googleR4Codec(t)
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
		"telecom": [
			{"system": "phone", "value": "555-0100"},
			{"system": "phone", "value": "555-0101"},
			{"system": "email", "value": "jane@example.com"}
		]
	}`)
	return envelopeFromJSON(t, "Patient", data)
}

func patientJohn(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType": "Patient",
		"id": "pat-john",
		"gender": "male",
		"name": [{"given": ["John"], "family": "Smith"}],
		"active": false
	}`)
	return envelopeFromJSON(t, "Patient", data)
}

func appointmentBooked(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType": "Appointment",
		"id": "appt-1",
		"status": "booked",
		"description": "Annual checkup",
		"start": "2024-06-15T09:00:00Z",
		"participant": [{"actor": {"reference": "Patient/pat-jane"}, "status": "accepted"}]
	}`)
	return envelopeFromJSON(t, "Appointment", data)
}

func appointmentFulfilled(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType": "Appointment",
		"id": "appt-2",
		"status": "fulfilled",
		"description": "Follow-up",
		"start": "2024-01-10T10:00:00Z",
		"participant": [{"actor": {"reference": "Patient/pat-john"}, "status": "accepted"}]
	}`)
	return envelopeFromJSON(t, "Appointment", data)
}

func observationHeartRate(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType": "Observation",
		"id": "obs-1",
		"status": "final",
		"code": {"text": "Heart rate"},
		"valueQuantity": {"value": 72, "unit": "beats/min"}
	}`)
	return envelopeFromJSON(t, "Observation", data)
}

func observationDraft(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType": "Observation",
		"id": "obs-2",
		"status": "preliminary",
		"code": {"text": "Body temperature"},
		"valueQuantity": {"value": 37.1, "unit": "C"}
	}`)
	return envelopeFromJSON(t, "Observation", data)
}

func viewWithUnsupportedJoin() []byte {
	return []byte(`{
		"resourceType": "ViewDefinition",
		"name": "bad_join_view",
		"version": "1.0.0",
		"resource": "Patient",
		"select": [
			{"column": [{"name": "id", "path": "Patient.id"}]},
			{"forEach": "Patient.generalPractitioner", "column": [{"name": "gp", "path": "$this.reference"}]}
		]
	}`)
}

func viewWithNestedSelect() []byte {
	return []byte(`{
		"resourceType": "ViewDefinition",
		"name": "bad_nested_view",
		"version": "1.0.0",
		"resource": "Patient",
		"select": [{
			"select": [{"column": [{"name": "id", "path": "Patient.id"}]}]
		}]
	}`)
}
