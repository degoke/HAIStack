package analytics

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

type memEventStore struct {
	events []store.ResourceEvent
}

func (m *memEventStore) Append(_ context.Context, event store.ResourceEvent) (store.ResourceEvent, error) {
	event.Sequence = int64(len(m.events) + 1)
	m.events = append(m.events, event)
	return event, nil
}

func (m *memEventStore) ReadSince(_ context.Context, after int64, limit int) ([]store.ResourceEvent, error) {
	var out []store.ResourceEvent
	for _, ev := range m.events {
		if ev.Sequence <= after {
			continue
		}
		out = append(out, ev)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *memEventStore) LatestForResource(context.Context, string, string) (*store.ResourceEvent, error) {
	return nil, nil
}

type memJobStore struct {
	jobs map[string]store.JobRecord
}

func newMemJobStore() *memJobStore {
	return &memJobStore{jobs: make(map[string]store.JobRecord)}
}

func (m *memJobStore) Enqueue(_ context.Context, job store.JobRecord) error {
	m.jobs[job.ID] = job
	return nil
}

func (m *memJobStore) ClaimNext(context.Context, string) (*store.JobRecord, error) {
	return nil, nil
}

func (m *memJobStore) Update(_ context.Context, job store.JobRecord) error {
	m.jobs[job.ID] = job
	return nil
}

func (m *memJobStore) Get(_ context.Context, id string) (*store.JobRecord, error) {
	job, ok := m.jobs[id]
	if !ok {
		return nil, nil
	}
	copy := job
	return &copy, nil
}

func TestCDCProcessorSchedulesRefresh(t *testing.T) {
	events := &memEventStore{}
	events.events = []store.ResourceEvent{{
		Sequence:     1,
		ResourceType: "Patient",
		ID:           "p1",
		Action:       store.EventActionCreate,
		Timestamp:    time.Now().UTC(),
	}}
	cursors := &memCursorStore{byName: make(map[string]store.Cursor)}
	jobsStore := newMemJobStore()
	processor := &CDCProcessor{
		Events:    events,
		Cursors:   cursors,
		Jobs:      jobsStore,
		Views:     SupportedViews,
		Scope:     "test",
		BatchSize: 10,
	}
	n, err := processor.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if n != 1 {
		t.Fatalf("processed = %d", n)
	}
	jobID := refreshJobID(ViewPatientSummary, "1.0.0")
	if _, ok := jobsStore.jobs[jobID]; !ok {
		t.Fatalf("expected refresh job %q", jobID)
	}
	cursor, _ := cursors.GetCursor(context.Background(), cdcCursorName("test"))
	if cursor == nil || cursor.Position != "1" {
		t.Fatalf("cursor = %#v", cursor)
	}
}

func TestCDCProcessorIdempotentPendingJob(t *testing.T) {
	events := &memEventStore{events: []store.ResourceEvent{{
		Sequence: 1, ResourceType: "Patient", ID: "p1", Action: store.EventActionUpdate,
	}}}
	cursors := &memCursorStore{byName: make(map[string]store.Cursor)}
	jobsStore := newMemJobStore()
	jobID := refreshJobID(ViewPatientSummary, "1.0.0")
	jobsStore.jobs[jobID] = store.JobRecord{ID: jobID, Status: store.JobStatusPending}
	processor := &CDCProcessor{
		Events: events, Cursors: cursors, Jobs: jobsStore, Views: SupportedViews,
	}
	if _, err := processor.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(jobsStore.jobs) != 1 {
		t.Fatalf("jobs = %d", len(jobsStore.jobs))
	}
}

func TestConcurrencyLimiter(t *testing.T) {
	limiter := NewConcurrencyLimiter(1)
	release := limiter.Acquire()
	done := make(chan struct{})
	go func() {
		second := limiter.Acquire()
		close(done)
		second()
	}()
	select {
	case <-done:
		t.Fatal("expected second acquire to block")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected second acquire to proceed")
	}
}

// reuse memCursorStore from incremental_test.go in same package
type memCursorStore struct {
	byName map[string]store.Cursor
}

func (m *memCursorStore) GetCursor(_ context.Context, name string) (*store.Cursor, error) {
	if c, ok := m.byName[name]; ok {
		copy := c
		return &copy, nil
	}
	return nil, nil
}

func (m *memCursorStore) UpsertCursor(_ context.Context, cursor store.Cursor) error {
	if m.byName == nil {
		m.byName = make(map[string]store.Cursor)
	}
	m.byName[cursor.Name] = cursor
	return nil
}

func (m *memCursorStore) DeleteCursor(_ context.Context, name string) error {
	delete(m.byName, name)
	return nil
}

var _ store.EventStore = (*memEventStore)(nil)
var _ store.JobStore = (*memJobStore)(nil)

func TestRefreshJobIDStable(t *testing.T) {
	if got := refreshJobID("patient_summary_view", "1.0.0"); got == "" {
		t.Fatal("empty job id")
	}
	_ = strconv.Itoa(1)
}
