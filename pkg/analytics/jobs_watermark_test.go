package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/view"
)

type errReportingWriter struct{}

func (errReportingWriter) writeAt(context.Context, *view.Result, time.Time) error {
	return errors.New("write failed")
}

type memReportingTableStore struct {
	rows []map[string]any
}

func (s *memReportingTableStore) Refresh(_ context.Context, _ store.ReportingTableMeta, rows []map[string]any) error {
	s.rows = append([]map[string]any(nil), rows...)
	return nil
}

func (s *memReportingTableStore) QueryRows(context.Context, string, string) ([]map[string]any, error) {
	return append([]map[string]any(nil), s.rows...), nil
}

func (s *memReportingTableStore) GetMeta(context.Context, string, string) (*store.ReportingTableMeta, error) {
	return &store.ReportingTableMeta{}, nil
}

func watermarkTestExecutor(t *testing.T) *view.Executor {
	t.Helper()
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), engine); err != nil {
		t.Fatalf("register: %v", err)
	}
	resources := watermarkMemResourceStore{}
	resources.seed(t)
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    engine,
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("executor: %v", err)
	}
	return exec
}

type watermarkMemResourceStore struct {
	data map[string]*types.ResourceEnvelope
}

func (s *watermarkMemResourceStore) seed(t *testing.T) {
	t.Helper()
	if s.data == nil {
		s.data = make(map[string]*types.ResourceEnvelope)
	}
	codec := types.NewJSONCodec()
	env, err := codec.ParseJSON("Patient", []byte(`{"resourceType":"Patient","id":"jane","name":[{"family":"Doe"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	s.data["Patient/jane"] = env
}

func (s watermarkMemResourceStore) Create(context.Context, *types.ResourceEnvelope) error {
	return errors.New("not implemented")
}
func (s watermarkMemResourceStore) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	res, ok := s.data[resourceType+"/"+id]
	if !ok {
		return nil, errors.New("not found")
	}
	return res, nil
}
func (s watermarkMemResourceStore) Update(context.Context, *types.ResourceEnvelope) error {
	return errors.New("not implemented")
}
func (s watermarkMemResourceStore) Delete(context.Context, string, string) error {
	return errors.New("not implemented")
}
func (s watermarkMemResourceStore) Exists(_ context.Context, resourceType, id string) (bool, error) {
	_, ok := s.data[resourceType+"/"+id]
	return ok, nil
}
func (s watermarkMemResourceStore) ListIDs(_ context.Context, resourceType string, limit, offset int) ([]string, error) {
	if offset > 0 {
		return nil, nil
	}
	var ids []string
	for key := range s.data {
		if len(ids) >= limit && limit > 0 {
			break
		}
		rt, id, _ := splitKey(key)
		if rt == resourceType {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func splitKey(key string) (string, string, bool) {
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			return key[:i], key[i+1:], true
		}
	}
	return "", "", false
}

func TestRefreshHandlerAdvancesWatermarkAfterSuccess(t *testing.T) {
	ctx := context.Background()
	cursors := &memCursorStore{byName: make(map[string]store.Cursor)}
	watermarks := NewWatermarkStore(cursors)
	refreshAt := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	runner, err := NewRunner(Config{
		Executor: watermarkTestExecutor(t),
		Now:      func() time.Time { return refreshAt },
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	payload, _ := json.Marshal(RefreshPayload{ViewName: ViewPatientSummary, Version: "1.0.0"})
	handler := RefreshHandler(runner, NewReportingTarget(&memReportingTableStore{}), watermarks)
	if err := handler.HandleJob(ctx, store.JobRecord{Payload: payload}); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	since, err := watermarks.Since(ctx, ViewPatientSummary, "1.0.0")
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if !since.Equal(refreshAt) {
		t.Fatalf("watermark = %v, want %v", since, refreshAt)
	}
}

func TestRefreshHandlerDoesNotAdvanceWatermarkOnFailure(t *testing.T) {
	ctx := context.Background()
	cursors := &memCursorStore{byName: make(map[string]store.Cursor)}
	watermarks := NewWatermarkStore(cursors)
	runner, err := NewRunner(Config{Executor: watermarkTestExecutor(t)})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	payload, _ := json.Marshal(RefreshPayload{ViewName: ViewPatientSummary, Version: "1.0.0"})
	handler := RefreshHandler(runner, errReportingWriter{}, watermarks)
	if err := handler.HandleJob(ctx, store.JobRecord{Payload: payload}); err == nil {
		t.Fatal("expected refresh failure")
	}

	since, err := watermarks.Since(ctx, ViewPatientSummary, "1.0.0")
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if !since.IsZero() {
		t.Fatalf("watermark = %v, want zero", since)
	}
}

func TestCDCProcessorDoesNotAdvanceWatermark(t *testing.T) {
	ctx := context.Background()
	events := &memEventStore{events: []store.ResourceEvent{{
		Sequence: 1, ResourceType: "Patient", ID: "p1", Action: store.EventActionCreate,
	}}}
	cursors := &memCursorStore{byName: make(map[string]store.Cursor)}
	jobsStore := newMemJobStore()
	watermarks := NewWatermarkStore(cursors)
	processor := &CDCProcessor{
		Events: events, Cursors: cursors, Jobs: jobsStore, Views: SupportedViews,
	}
	if _, err := processor.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	since, err := watermarks.Since(ctx, ViewPatientSummary, "1.0.0")
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if !since.IsZero() {
		t.Fatalf("CDC advanced watermark to %v", since)
	}
}

var _ store.ReportingTableStore = (*memReportingTableStore)(nil)
var _ store.ResourceStore = (*watermarkMemResourceStore)(nil)
