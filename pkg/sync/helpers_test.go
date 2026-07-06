package sync_test

import (
	"context"
	"sync"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/google/uuid"
)

type memInboxStore struct {
	mu   sync.Mutex
	data map[string]time.Time
}

func newMemInboxStore() *memInboxStore {
	return &memInboxStore{data: make(map[string]time.Time)}
}

func (s *memInboxStore) MarkApplied(_ context.Context, id string, appliedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[id] = appliedAt
	return nil
}

func (s *memInboxStore) IsApplied(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[id]
	return ok, nil
}

func (s *memInboxStore) AppliedAt(_ context.Context, id string) (*time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts, ok := s.data[id]
	if !ok {
		return nil, nil
	}
	copy := ts
	return &copy, nil
}

type memHub struct {
	mu              sync.Mutex
	processed       map[string]hasync.PushResult
	canonical       []hasync.CanonicalEvent
	nextSeq         int64
	pushErr         error
	resources       map[string]*types.ResourceEnvelope
	staleOnMismatch bool
}

func newMemHub() *memHub {
	return &memHub{
		processed: make(map[string]hasync.PushResult),
		resources: make(map[string]*types.ResourceEnvelope),
	}
}

func resourceKey(resourceType, id string) string {
	return resourceType + "/" + id
}

func (h *memHub) Push(_ context.Context, events []hasync.LocalEvent) ([]hasync.PushResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pushErr != nil {
		return nil, h.pushErr
	}

	results := make([]hasync.PushResult, 0, len(events))
	for _, event := range events {
		if prior, ok := h.processed[event.EventID]; ok {
			results = append(results, hasync.PushResult{
				EventID: event.EventID,
				State:   hasync.AckAlreadyProcessed,
				CanonicalSequence: prior.CanonicalSequence,
				CanonicalVersionID: prior.CanonicalVersionID,
			})
			continue
		}

		key := resourceKey(event.ResourceType, event.ResourceID)
		current := h.resources[key]

		if event.Operation == hasync.EventTypeResourceCreated && current != nil {
			results = append(results, hasync.PushResult{
				EventID:                 event.EventID,
				State:                   hasync.AckConflicted,
				ConflictReason:          "resource already exists",
				ConflictRemoteVersionID: current.VersionID,
			})
			continue
		}

		if event.Operation != hasync.EventTypeResourceCreated {
			if current == nil {
				results = append(results, hasync.PushResult{
					EventID:        event.EventID,
					State:          hasync.AckConflicted,
					ConflictReason: "resource not found",
				})
				continue
			}
			if h.staleOnMismatch && event.BaseCloudVersion != "" && event.BaseCloudVersion != current.VersionID {
				results = append(results, hasync.PushResult{
					EventID:                 event.EventID,
					State:                   hasync.AckConflicted,
					ConflictReason:          "stale base version",
					ConflictRemoteVersionID: current.VersionID,
				})
				continue
			}
		}

		if event.Operation == hasync.EventTypeResourceDeleted {
			delete(h.resources, key)
			h.nextSeq++
			versionID := uuid.NewString()
			result := hasync.PushResult{
				EventID:            event.EventID,
				State:              hasync.AckAccepted,
				CanonicalSequence:  h.nextSeq,
				CanonicalVersionID: versionID,
			}
			h.processed[event.EventID] = result
			h.canonical = append(h.canonical, hasync.CanonicalEvent{
				TenantID:           event.TenantID,
				ResourceType:       event.ResourceType,
				ResourceID:         event.ResourceID,
				Operation:          event.Operation,
				ResourceHash:       event.ResourceHash,
				CanonicalSequence:  h.nextSeq,
				CanonicalVersionID: versionID,
				Status:             hasync.CanonicalStatusAccepted,
			})
			results = append(results, result)
			continue
		}

		if event.ResourceAfter == nil {
			results = append(results, hasync.PushResult{
				EventID:         event.EventID,
				State:           hasync.AckRejected,
				RejectionReason: "missing resource payload",
			})
			continue
		}

		res := *event.ResourceAfter
		res.VersionID = uuid.NewString()
		h.resources[key] = &res
		h.nextSeq++
		result := hasync.PushResult{
			EventID:            event.EventID,
			State:              hasync.AckAccepted,
			CanonicalSequence:  h.nextSeq,
			CanonicalVersionID: res.VersionID,
		}
		h.processed[event.EventID] = result
		h.canonical = append(h.canonical, hasync.CanonicalEvent{
			TenantID:           event.TenantID,
			ResourceType:       event.ResourceType,
			ResourceID:         event.ResourceID,
			Operation:          event.Operation,
			ResourceAfter:      &res,
			ResourceHash:       res.Hash,
			CanonicalSequence:  h.nextSeq,
			CanonicalVersionID: res.VersionID,
			Status:             hasync.CanonicalStatusAccepted,
		})
		results = append(results, result)
	}
	return results, nil
}

func (h *memHub) Pull(_ context.Context, afterSequence int64, limit int) ([]hasync.CanonicalEvent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []hasync.CanonicalEvent
	for _, event := range h.canonical {
		if event.CanonicalSequence <= afterSequence {
			continue
		}
		out = append(out, event)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

type memEventStore struct {
	mu       sync.Mutex
	events   []store.ResourceEvent
	sequence int64
}

func (s *memEventStore) Append(_ context.Context, event store.ResourceEvent) (store.ResourceEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence++
	event.Sequence = s.sequence
	s.events = append(s.events, event)
	return event, nil
}

func (s *memEventStore) ReadSince(_ context.Context, afterSequence int64, limit int) ([]store.ResourceEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.ResourceEvent
	for _, event := range s.events {
		if event.Sequence <= afterSequence {
			continue
		}
		out = append(out, event)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *memEventStore) LatestForResource(_ context.Context, resourceType, id string) (*store.ResourceEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.events) - 1; i >= 0; i-- {
		event := s.events[i]
		if event.ResourceType == resourceType && event.ID == id {
			copy := event
			return &copy, nil
		}
	}
	return nil, nil
}

type memCursorStore struct {
	mu   sync.Mutex
	data map[string]store.Cursor
}

func newMemCursorStore() *memCursorStore {
	return &memCursorStore{data: make(map[string]store.Cursor)}
}

func (s *memCursorStore) GetCursor(_ context.Context, name string) (*store.Cursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cursor, ok := s.data[name]
	if !ok {
		return nil, nil
	}
	copy := cursor
	return &copy, nil
}

func (s *memCursorStore) UpsertCursor(_ context.Context, cursor store.Cursor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[cursor.Name] = cursor
	return nil
}

func (s *memCursorStore) DeleteCursor(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, name)
	return nil
}

type memResourceStore struct {
	mu   sync.Mutex
	data map[string]*types.ResourceEnvelope
}

func newMemResourceStore() *memResourceStore {
	return &memResourceStore{data: make(map[string]*types.ResourceEnvelope)}
}

func (s *memResourceStore) Create(_ context.Context, res *types.ResourceEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[resourceKey(res.ResourceType, res.ID)] = res
	return nil
}

func (s *memResourceStore) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, ok := s.data[resourceKey(resourceType, id)]
	if !ok {
		return nil, nil
	}
	copy := *res
	return &copy, nil
}

func (s *memResourceStore) Update(_ context.Context, res *types.ResourceEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[resourceKey(res.ResourceType, res.ID)] = res
	return nil
}

func (s *memResourceStore) Delete(_ context.Context, resourceType, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, resourceKey(resourceType, id))
	return nil
}

func (s *memResourceStore) Exists(_ context.Context, resourceType, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[resourceKey(resourceType, id)]
	return ok, nil
}

func (s *memResourceStore) ListIDs(context.Context, string, int, int) ([]string, error) {
	return nil, nil
}

type memHistoryStore struct {
	mu   sync.Mutex
	data map[string][]store.ResourceVersion
}

func newMemHistoryStore() *memHistoryStore {
	return &memHistoryStore{data: make(map[string][]store.ResourceVersion)}
}

func (s *memHistoryStore) AppendVersion(_ context.Context, version store.ResourceVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceKey(version.ResourceType, version.ID)
	s.data[key] = append(s.data[key], version)
	return nil
}

func (s *memHistoryStore) GetHistory(_ context.Context, resourceType, id string) ([]store.ResourceVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	history := s.data[resourceKey(resourceType, id)]
	out := make([]store.ResourceVersion, len(history))
	copy(out, history)
	return out, nil
}

type memConflictStore struct {
	mu      sync.Mutex
	records []store.ConflictRecord
}

func (s *memConflictStore) Append(_ context.Context, record store.ConflictRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	return nil
}

func (s *memConflictStore) List(context.Context, string, string) ([]store.ConflictRecord, error) {
	return nil, nil
}

func (s *memConflictStore) Resolve(context.Context, string, time.Time) error {
	return nil
}

type memJobStore struct {
	mu   sync.Mutex
	jobs []store.JobRecord
}

func (s *memJobStore) Enqueue(_ context.Context, job store.JobRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, job)
	return nil
}

func (s *memJobStore) ClaimNext(_ context.Context, jobType string) (*store.JobRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, job := range s.jobs {
		if job.Type != jobType || job.Status != store.JobStatusPending {
			continue
		}
		job.Status = store.JobStatusRunning
		s.jobs[i] = job
		copy := job
		return &copy, nil
	}
	return nil, nil
}

func (s *memJobStore) Update(_ context.Context, job store.JobRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.jobs {
		if existing.ID == job.ID {
			s.jobs[i] = job
			return nil
		}
	}
	return nil
}
func (s *memJobStore) Get(context.Context, string) (*store.JobRecord, error) {
	return nil, nil
}

type memAuditStore struct {
	mu      sync.Mutex
	records []store.AuditRecord
}

func (s *memAuditStore) Append(_ context.Context, record store.AuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	return nil
}

func (s *memAuditStore) List(context.Context, store.AuditQuery) ([]store.AuditRecord, error) {
	return nil, nil
}

func fixedClock(t time.Time) hasync.Clock {
	return func() time.Time { return t }
}

func sampleResource(id, version string) *types.ResourceEnvelope {
	return &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           id,
		VersionID:    version,
		LastUpdated:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		JSON:         []byte(`{"resourceType":"Patient","id":"` + id + `"}`),
		Hash:         "hash-" + id,
	}
}
