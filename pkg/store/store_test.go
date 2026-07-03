package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

var (
	_ store.ResourceStore         = (*memResourceStore)(nil)
	_ store.HistoryStore          = (*memHistoryStore)(nil)
	_ store.SearchStore           = (*memSearchStore)(nil)
	_ store.EventStore            = (*memEventStore)(nil)
	_ store.Transactor            = (*memTransactor)(nil)
	_ store.BinaryStore           = (*memBinaryStore)(nil)
	_ store.BlobStore             = (*memBlobStore)(nil)
	_ store.CursorStore           = (*memCursorStore)(nil)
	_ store.ConflictStore         = (*memConflictStore)(nil)
	_ store.AnalyticsStore        = (*memAnalyticsStore)(nil)
	_ store.AuditStore            = (*memAuditStore)(nil)
	_ store.JobStore              = (*memJobStore)(nil)
	_ store.ModuleStore           = (*memModuleStore)(nil)
	_ store.MaterializedViewStore = (*memMaterializedViewStore)(nil)
	_ store.WriteSessionProvider  = (*memWriteSessionProvider)(nil)
	_ store.IDRegistryStore       = (*memIDRegistryStore)(nil)
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
	for key, res := range s.data {
		if res.ResourceType != resourceType {
			continue
		}
		ids = append(ids, res.ID)
		_ = key
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

type memEventStore struct {
	mu       sync.Mutex
	events   []store.ResourceEvent
	sequence int64
}

func newMemEventStore() *memEventStore {
	return &memEventStore{}
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

type memSearchStore struct {
	mu      sync.Mutex
	entries map[string]store.SearchIndexEntry
	lookups map[string][]string
}

func newMemSearchStore() *memSearchStore {
	return &memSearchStore{
		entries: make(map[string]store.SearchIndexEntry),
		lookups: make(map[string][]string),
	}
}

func (s *memSearchStore) Index(_ context.Context, entry store.SearchIndexEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceKey(entry.ResourceType, entry.ID)
	s.entries[key] = entry
	for field, value := range entry.Fields {
		lookupKey := field + "=" + value
		s.lookups[lookupKey] = appendUnique(s.lookups[lookupKey], entry.ID)
	}
	return nil
}

func (s *memSearchStore) RemoveIndex(_ context.Context, resourceType, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, resourceKey(resourceType, id))
	return nil
}

func (s *memSearchStore) Lookup(_ context.Context, key, value string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.lookups[key+"="+value]
	out := make([]string, len(ids))
	copy(out, ids)
	return out, nil
}

func (s *memSearchStore) QueryPrepared(_ context.Context, query store.PreparedQuery, args map[string]string) ([]string, error) {
	if query.Name == "by-field" {
		return s.Lookup(context.Background(), args["key"], args["value"])
	}
	return nil, nil
}

func appendUnique(ids []string, id string) []string {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}

type memTransaction struct {
	committed  bool
	rolledBack bool
}

func (tx *memTransaction) Commit() error {
	if tx.rolledBack {
		return errors.New("transaction already rolled back")
	}
	tx.committed = true
	return nil
}

func (tx *memTransaction) Rollback() error {
	if tx.committed {
		return errors.New("transaction already committed")
	}
	tx.rolledBack = true
	return nil
}

type memTransactor struct{}

func (memTransactor) BeginTx(_ context.Context) (store.Transaction, error) {
	return &memTransaction{}, nil
}

type memJobStore struct {
	mu   sync.Mutex
	jobs map[string]store.JobRecord
}

func newMemJobStore() *memJobStore {
	return &memJobStore{jobs: make(map[string]store.JobRecord)}
}

func (s *memJobStore) Enqueue(_ context.Context, job store.JobRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
	return nil
}

func (s *memJobStore) ClaimNext(_ context.Context, jobType string) (*store.JobRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, job := range s.jobs {
		if job.Type == jobType && job.Status == store.JobStatusPending {
			job.Status = store.JobStatusRunning
			s.jobs[id] = job
			copy := job
			return &copy, nil
		}
	}
	return nil, nil
}

func (s *memJobStore) Update(_ context.Context, job store.JobRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
	return nil
}

func (s *memJobStore) Get(_ context.Context, id string) (*store.JobRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job not found: %s", id)
	}
	copy := job
	return &copy, nil
}

type memModuleStore struct {
	mu      sync.Mutex
	modules map[string]store.ModuleRecord
}

func newMemModuleStore() *memModuleStore {
	return &memModuleStore{modules: make(map[string]store.ModuleRecord)}
}

func (s *memModuleStore) Register(_ context.Context, module store.ModuleRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modules[module.Name] = module
	return nil
}

func (s *memModuleStore) Get(_ context.Context, name string) (*store.ModuleRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	module, ok := s.modules[name]
	if !ok {
		return nil, fmt.Errorf("module not found: %s", name)
	}
	copy := module
	return &copy, nil
}

func (s *memModuleStore) List(_ context.Context) ([]store.ModuleRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.ModuleRecord, 0, len(s.modules))
	for _, module := range s.modules {
		out = append(out, module)
	}
	return out, nil
}

func (s *memModuleStore) Unregister(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.modules, name)
	return nil
}

type memMaterializedViewStore struct {
	mu    sync.Mutex
	views map[string]store.MaterializedViewRecord
}

func newMemMaterializedViewStore() *memMaterializedViewStore {
	return &memMaterializedViewStore{views: make(map[string]store.MaterializedViewRecord)}
}

func viewKey(viewName, key string) string {
	return viewName + "/" + key
}

func (s *memMaterializedViewStore) Upsert(_ context.Context, record store.MaterializedViewRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.views[viewKey(record.ViewName, record.Key)] = record
	return nil
}

func (s *memMaterializedViewStore) Get(_ context.Context, viewName, key string) (*store.MaterializedViewRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.views[viewKey(viewName, key)]
	if !ok {
		return nil, fmt.Errorf("view record not found: %s/%s", viewName, key)
	}
	copy := record
	return &copy, nil
}

func (s *memMaterializedViewStore) Delete(_ context.Context, viewName, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.views, viewKey(viewName, key))
	return nil
}

func (s *memMaterializedViewStore) ListKeys(_ context.Context, viewName string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := viewName + "/"
	var keys []string
	for compositeKey := range s.views {
		if len(compositeKey) >= len(prefix) && compositeKey[:len(prefix)] == prefix {
			keys = append(keys, compositeKey[len(prefix):])
		}
	}
	return keys, nil
}

type memBinaryStore struct {
	mu   sync.Mutex
	data map[string]store.BinaryObject
}

func newMemBinaryStore() *memBinaryStore {
	return &memBinaryStore{data: make(map[string]store.BinaryObject)}
}

func (s *memBinaryStore) Put(_ context.Context, obj store.BinaryObject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[obj.Key] = obj
	return nil
}

func (s *memBinaryStore) Get(_ context.Context, key string) (*store.BinaryObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("binary not found: %s", key)
	}
	copy := obj
	return &copy, nil
}

func (s *memBinaryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

type memBlobStore struct {
	mu   sync.Mutex
	data map[string]store.BlobObject
}

func newMemBlobStore() *memBlobStore {
	return &memBlobStore{data: make(map[string]store.BlobObject)}
}

func (s *memBlobStore) Put(_ context.Context, obj store.BlobObject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[obj.Key] = obj
	return nil
}

func (s *memBlobStore) Get(_ context.Context, key string) (*store.BlobObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("blob not found: %s", key)
	}
	copy := obj
	return &copy, nil
}

func (s *memBlobStore) Head(_ context.Context, key string) (*store.BlobObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("blob not found: %s", key)
	}
	head := obj
	head.Data = nil
	return &head, nil
}

func (s *memBlobStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

type memCursorStore struct {
	mu      sync.Mutex
	cursors map[string]store.Cursor
}

func newMemCursorStore() *memCursorStore {
	return &memCursorStore{cursors: make(map[string]store.Cursor)}
}

func (s *memCursorStore) GetCursor(_ context.Context, name string) (*store.Cursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cursor, ok := s.cursors[name]
	if !ok {
		return nil, fmt.Errorf("cursor not found: %s", name)
	}
	copy := cursor
	return &copy, nil
}

func (s *memCursorStore) UpsertCursor(_ context.Context, cursor store.Cursor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cursors[cursor.Name] = cursor
	return nil
}

func (s *memCursorStore) DeleteCursor(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cursors, name)
	return nil
}

type memConflictStore struct {
	mu         sync.Mutex
	records    map[string]store.ConflictRecord
	byResource map[string][]string
}

func newMemConflictStore() *memConflictStore {
	return &memConflictStore{
		records:    make(map[string]store.ConflictRecord),
		byResource: make(map[string][]string),
	}
}

func (s *memConflictStore) Append(_ context.Context, record store.ConflictRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.ID] = record
	key := resourceKey(record.ResourceType, record.ResourceID)
	s.byResource[key] = append(s.byResource[key], record.ID)
	return nil
}

func (s *memConflictStore) List(_ context.Context, resourceType, resourceID string) ([]store.ConflictRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.byResource[resourceKey(resourceType, resourceID)]
	out := make([]store.ConflictRecord, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.records[id])
	}
	return out, nil
}

func (s *memConflictStore) Resolve(_ context.Context, id string, resolvedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return fmt.Errorf("conflict not found: %s", id)
	}
	record.ResolvedAt = &resolvedAt
	s.records[id] = record
	return nil
}

type memAnalyticsStore struct {
	mu     sync.Mutex
	events []store.AnalyticsEvent
}

func newMemAnalyticsStore() *memAnalyticsStore {
	return &memAnalyticsStore{}
}

func (s *memAnalyticsStore) Append(_ context.Context, event store.AnalyticsEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func (s *memAnalyticsStore) QueryPrepared(_ context.Context, query store.PreparedQuery, args map[string]string) ([]store.AnalyticsEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if query.Name != "by-name" {
		return nil, nil
	}
	name := args["name"]
	var out []store.AnalyticsEvent
	for _, event := range s.events {
		if event.Name == name {
			out = append(out, event)
		}
	}
	return out, nil
}

type memAuditStore struct {
	mu      sync.Mutex
	records []store.AuditRecord
}

func newMemAuditStore() *memAuditStore {
	return &memAuditStore{}
}

func (s *memAuditStore) Append(_ context.Context, record store.AuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	return nil
}

func (s *memAuditStore) List(_ context.Context, query store.AuditQuery) ([]store.AuditRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.AuditRecord
	for _, record := range s.records {
		if query.ResourceType != "" && record.ResourceType != query.ResourceType {
			continue
		}
		if query.ResourceID != "" && record.ResourceID != query.ResourceID {
			continue
		}
		if query.Actor != "" && record.Actor != query.Actor {
			continue
		}
		if !query.After.IsZero() && record.Timestamp.Before(query.After) {
			continue
		}
		if !query.Before.IsZero() && record.Timestamp.After(query.Before) {
			continue
		}
		out = append(out, record)
		if query.Limit > 0 && len(out) >= query.Limit {
			break
		}
	}
	return out, nil
}

type memWriteSession struct {
	resources  *memResourceStore
	history    *memHistoryStore
	search     *memSearchStore
	events     *memEventStore
	committed  bool
	rolledBack bool
}

func (s *memWriteSession) ResourceStore() store.ResourceStore { return s.resources }
func (s *memWriteSession) HistoryStore() store.HistoryStore   { return s.history }
func (s *memWriteSession) SearchStore() store.SearchStore     { return s.search }
func (s *memWriteSession) EventStore() store.EventStore       { return s.events }

func (s *memWriteSession) Commit(_ context.Context) error {
	if s.rolledBack {
		return errors.New("write session already rolled back")
	}
	s.committed = true
	return nil
}

func (s *memWriteSession) Rollback(_ context.Context) error {
	if s.committed {
		return errors.New("write session already committed")
	}
	s.rolledBack = true
	return nil
}

type memWriteSessionProvider struct {
	resources *memResourceStore
	history   *memHistoryStore
	search    *memSearchStore
	events    *memEventStore
}

func newMemWriteSessionProvider() *memWriteSessionProvider {
	return &memWriteSessionProvider{
		resources: newMemResourceStore(),
		history:   newMemHistoryStore(),
		search:    newMemSearchStore(),
		events:    newMemEventStore(),
	}
}

func (p *memWriteSessionProvider) BeginWrite(_ context.Context) (store.WriteSession, error) {
	return &memWriteSession{
		resources: p.resources,
		history:   p.history,
		search:    p.search,
		events:    p.events,
	}, nil
}

type memIDRegistryStore struct {
	mu         sync.Mutex
	registered map[string]struct{}
	reserved   map[string]struct{}
}

func newMemIDRegistryStore() *memIDRegistryStore {
	return &memIDRegistryStore{
		registered: make(map[string]struct{}),
		reserved:   make(map[string]struct{}),
	}
}

func (s *memIDRegistryStore) Check(_ context.Context, resourceType, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, registered := s.registered[resourceKey(resourceType, id)]
	return registered, nil
}

func (s *memIDRegistryStore) Reserve(_ context.Context, resourceType, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceKey(resourceType, id)
	if _, ok := s.registered[key]; ok {
		return fmt.Errorf("id already registered: %s", key)
	}
	if _, ok := s.reserved[key]; ok {
		return fmt.Errorf("id already reserved: %s", key)
	}
	s.reserved[key] = struct{}{}
	return nil
}

func (s *memIDRegistryStore) Register(_ context.Context, resourceType, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceKey(resourceType, id)
	delete(s.reserved, key)
	s.registered[key] = struct{}{}
	return nil
}

func TestResourceVersionZeroValueJSON(t *testing.T) {
	var version store.ResourceVersion
	data, err := json.Marshal(version)
	if err != nil {
		t.Fatalf("Marshal zero value: %v", err)
	}

	var decoded store.ResourceVersion
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal zero value: %v", err)
	}
	if decoded.Action != "" {
		t.Errorf("Action = %q, want empty", decoded.Action)
	}
	if !decoded.Timestamp.IsZero() {
		t.Error("Timestamp should be zero")
	}
	if decoded.Deleted {
		t.Error("Deleted should be false")
	}
}

func TestResourceVersionDeleteTombstone(t *testing.T) {
	ts := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	version := store.ResourceVersion{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "3",
		Action:       store.VersionActionDelete,
		Timestamp:    ts,
		Hash:         "abc123",
		Deleted:      true,
	}

	data, err := json.Marshal(version)
	if err != nil {
		t.Fatalf("Marshal tombstone: %v", err)
	}

	var decoded store.ResourceVersion
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal tombstone: %v", err)
	}
	if decoded.Action != store.VersionActionDelete {
		t.Errorf("Action = %q, want delete", decoded.Action)
	}
	if !decoded.Deleted {
		t.Error("Deleted should be true for tombstone history")
	}
	if decoded.Resource != nil {
		t.Error("Tombstone should not carry current resource payload")
	}
}

func TestResourceEventZeroValueJSON(t *testing.T) {
	var event store.ResourceEvent
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal zero value: %v", err)
	}

	var decoded store.ResourceEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal zero value: %v", err)
	}
	if decoded.Sequence != 0 {
		t.Errorf("Sequence = %d, want 0", decoded.Sequence)
	}
	if decoded.Action != "" {
		t.Errorf("Action = %q, want empty", decoded.Action)
	}
}

func TestEventActionConstants(t *testing.T) {
	if store.EventActionCreate != "create" {
		t.Errorf("EventActionCreate = %q", store.EventActionCreate)
	}
	if store.EventActionUpdate != "update" {
		t.Errorf("EventActionUpdate = %q", store.EventActionUpdate)
	}
	if store.EventActionDelete != "delete" {
		t.Errorf("EventActionDelete = %q", store.EventActionDelete)
	}
}

func TestVersionActionConstants(t *testing.T) {
	if store.VersionActionCreate != "create" {
		t.Errorf("VersionActionCreate = %q", store.VersionActionCreate)
	}
	if store.VersionActionUpdate != "update" {
		t.Errorf("VersionActionUpdate = %q", store.VersionActionUpdate)
	}
	if store.VersionActionDelete != "delete" {
		t.Errorf("VersionActionDelete = %q", store.VersionActionDelete)
	}
}

func TestCursorEmptyAndNonEmptyPositionJSON(t *testing.T) {
	empty := store.Cursor{Name: "indexer", Position: ""}
	emptyData, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("Marshal empty cursor: %v", err)
	}

	var decodedEmpty store.Cursor
	if err := json.Unmarshal(emptyData, &decodedEmpty); err != nil {
		t.Fatalf("Unmarshal empty cursor: %v", err)
	}
	if decodedEmpty.Position != "" {
		t.Errorf("Position = %q, want empty", decodedEmpty.Position)
	}

	nonEmpty := store.Cursor{
		Name:      "sync-worker",
		Position:  "42",
		UpdatedAt: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	nonEmptyData, err := json.Marshal(nonEmpty)
	if err != nil {
		t.Fatalf("Marshal non-empty cursor: %v", err)
	}

	var decodedNonEmpty store.Cursor
	if err := json.Unmarshal(nonEmptyData, &decodedNonEmpty); err != nil {
		t.Fatalf("Unmarshal non-empty cursor: %v", err)
	}
	if decodedNonEmpty.Position != "42" {
		t.Errorf("Position = %q, want 42", decodedNonEmpty.Position)
	}
}

func TestBinaryObjectZeroValueJSON(t *testing.T) {
	var obj store.BinaryObject
	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("Marshal zero value: %v", err)
	}

	var decoded store.BinaryObject
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal zero value: %v", err)
	}
	if decoded.Size != 0 {
		t.Errorf("Size = %d, want 0", decoded.Size)
	}
	if len(decoded.Data) != 0 {
		t.Errorf("Data length = %d, want 0", len(decoded.Data))
	}
}

func TestConflictRecordZeroValueJSON(t *testing.T) {
	var record store.ConflictRecord
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal zero value: %v", err)
	}

	var decoded store.ConflictRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal zero value: %v", err)
	}
	if decoded.ResolvedAt != nil {
		t.Error("ResolvedAt should be nil for zero value")
	}
}

func TestTransactorCommitAndRollback(t *testing.T) {
	var txr store.Transactor = memTransactor{}
	ctx := context.Background()

	tx, err := txr.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	tx, err = txr.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
}

func TestComposeResourceHistoryEventAndSearch(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	history := newMemHistoryStore()
	events := newMemEventStore()
	search := newMemSearchStore()

	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	envelope := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "1",
		LastUpdated:  now,
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"}`),
		Hash:         "hash-v1",
	}

	if err := resources.Create(ctx, envelope); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: envelope.ResourceType,
		ID:           envelope.ID,
		VersionID:    envelope.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    now,
		Resource:     envelope,
		Hash:         envelope.Hash,
	}); err != nil {
		t.Fatalf("AppendVersion: %v", err)
	}
	storedEvent, err := events.Append(ctx, store.ResourceEvent{
		ResourceType: envelope.ResourceType,
		ID:           envelope.ID,
		VersionID:    envelope.VersionID,
		Action:       store.EventActionCreate,
		Timestamp:    now,
		Hash:         envelope.Hash,
	})
	if err != nil {
		t.Fatalf("Append event: %v", err)
	}
	if storedEvent.Sequence != 1 {
		t.Errorf("event sequence = %d, want 1", storedEvent.Sequence)
	}
	if err := search.Index(ctx, store.SearchIndexEntry{
		ResourceType: envelope.ResourceType,
		ID:           envelope.ID,
		Fields:       map[string]string{"family": "Doe"},
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	exists, err := resources.Exists(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("expected resource to exist")
	}

	versions, err := history.GetHistory(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("history length = %d, want 1", len(versions))
	}

	readEvents, err := events.ReadSince(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(readEvents) != 1 {
		t.Fatalf("events length = %d, want 1", len(readEvents))
	}

	ids, err := search.Lookup(ctx, "family", "Doe")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(ids) != 1 || ids[0] != "pat-1" {
		t.Fatalf("Lookup ids = %v, want [pat-1]", ids)
	}

	if err := resources.Delete(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	deleteVersion := store.ResourceVersion{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "2",
		Action:       store.VersionActionDelete,
		Timestamp:    now.Add(time.Minute),
		Hash:         "hash-delete",
		Deleted:      true,
	}
	if err := history.AppendVersion(ctx, deleteVersion); err != nil {
		t.Fatalf("AppendVersion tombstone: %v", err)
	}
	if _, err := events.Append(ctx, store.ResourceEvent{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "2",
		Action:       store.EventActionDelete,
		Timestamp:    deleteVersion.Timestamp,
		Hash:         deleteVersion.Hash,
	}); err != nil {
		t.Fatalf("Append delete event: %v", err)
	}
	if err := search.RemoveIndex(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("RemoveIndex: %v", err)
	}

	exists, err = resources.Exists(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Fatal("expected resource to be deleted from current state")
	}

	versions, err = history.GetHistory(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("GetHistory after delete: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("history length = %d, want 2", len(versions))
	}
	last := versions[len(versions)-1]
	if last.Action != store.VersionActionDelete || !last.Deleted {
		t.Fatalf("last history entry = %+v, want delete tombstone", last)
	}
}

func TestBlobObjectZeroValueJSON(t *testing.T) {
	var obj store.BlobObject
	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("Marshal zero value: %v", err)
	}

	var decoded store.BlobObject
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal zero value: %v", err)
	}
	if decoded.Location != "" {
		t.Errorf("Location = %q, want empty", decoded.Location)
	}
}

func TestMaterializedViewRecordZeroValueJSON(t *testing.T) {
	var record store.MaterializedViewRecord
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal zero value: %v", err)
	}

	var decoded store.MaterializedViewRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal zero value: %v", err)
	}
	if decoded.Version != 0 {
		t.Errorf("Version = %d, want 0", decoded.Version)
	}
}

func TestAnalyticsEventZeroValueJSON(t *testing.T) {
	var event store.AnalyticsEvent
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal zero value: %v", err)
	}

	var decoded store.AnalyticsEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal zero value: %v", err)
	}
	if decoded.Name != "" {
		t.Errorf("Name = %q, want empty", decoded.Name)
	}
}

func TestJobRecordZeroValueJSON(t *testing.T) {
	var job store.JobRecord
	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("Marshal zero value: %v", err)
	}

	var decoded store.JobRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal zero value: %v", err)
	}
	if decoded.Status != "" {
		t.Errorf("Status = %q, want empty", decoded.Status)
	}
}

func TestJobStatusConstants(t *testing.T) {
	if store.JobStatusPending != "pending" {
		t.Errorf("JobStatusPending = %q", store.JobStatusPending)
	}
	if store.JobStatusRunning != "running" {
		t.Errorf("JobStatusRunning = %q", store.JobStatusRunning)
	}
	if store.JobStatusCompleted != "completed" {
		t.Errorf("JobStatusCompleted = %q", store.JobStatusCompleted)
	}
	if store.JobStatusFailed != "failed" {
		t.Errorf("JobStatusFailed = %q", store.JobStatusFailed)
	}
}

func TestAuditRecordZeroValueJSON(t *testing.T) {
	var record store.AuditRecord
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal zero value: %v", err)
	}

	var decoded store.AuditRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal zero value: %v", err)
	}
	if decoded.Outcome != "" {
		t.Errorf("Outcome = %q, want empty", decoded.Outcome)
	}
}

func TestModuleRecordZeroValueJSON(t *testing.T) {
	var module store.ModuleRecord
	data, err := json.Marshal(module)
	if err != nil {
		t.Fatalf("Marshal zero value: %v", err)
	}

	var decoded store.ModuleRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal zero value: %v", err)
	}
	if decoded.Version != "" {
		t.Errorf("Version = %q, want empty", decoded.Version)
	}
}

func TestJobStoreClaimNext(t *testing.T) {
	ctx := context.Background()
	jobs := newMemJobStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	if err := jobs.Enqueue(ctx, store.JobRecord{
		ID:        "job-1",
		Type:      "reindex",
		Status:    store.JobStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	claimed, err := jobs.ClaimNext(ctx, "reindex")
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected claimed job")
	}
	if claimed.Status != store.JobStatusRunning {
		t.Errorf("Status = %q, want running", claimed.Status)
	}
}

func TestModuleStoreRegisterAndList(t *testing.T) {
	ctx := context.Background()
	modules := newMemModuleStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	if err := modules.Register(ctx, store.ModuleRecord{
		Name:         "vitals",
		Version:      "1.0.0",
		Metadata:     map[string]string{"kind": "clinical"},
		RegisteredAt: now,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	list, err := modules.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Name != "vitals" {
		t.Fatalf("List = %+v, want one vitals module", list)
	}
}

func TestMaterializedViewStoreUpsertAndListKeys(t *testing.T) {
	ctx := context.Background()
	views := newMemMaterializedViewStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	if err := views.Upsert(ctx, store.MaterializedViewRecord{
		ViewName:  "patient-summary",
		Key:       "pat-1",
		Payload:   []byte(`{"active":true}`),
		Version:   1,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	keys, err := views.ListKeys(ctx, "patient-summary")
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 1 || keys[0] != "pat-1" {
		t.Fatalf("ListKeys = %v, want [pat-1]", keys)
	}
}

func TestBinaryStorePutGetDelete(t *testing.T) {
	ctx := context.Background()
	binaries := newMemBinaryStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	if err := binaries.Put(ctx, store.BinaryObject{
		Key:         "thumb-1",
		ContentType: "image/png",
		Size:        4,
		Data:        []byte{1, 2, 3, 4},
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	obj, err := binaries.Get(ctx, "thumb-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if obj.ContentType != "image/png" {
		t.Errorf("ContentType = %q, want image/png", obj.ContentType)
	}

	if err := binaries.Delete(ctx, "thumb-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := binaries.Get(ctx, "thumb-1"); err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestBlobStoreHeadOmitsData(t *testing.T) {
	ctx := context.Background()
	blobs := newMemBlobStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	if err := blobs.Put(ctx, store.BlobObject{
		Key:         "scan-1",
		ContentType: "application/pdf",
		Size:        3,
		Data:        []byte("pdf"),
		Location:    "opaque://scan-1",
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	head, err := blobs.Head(ctx, "scan-1")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Location != "opaque://scan-1" {
		t.Errorf("Location = %q, want opaque://scan-1", head.Location)
	}
	if len(head.Data) != 0 {
		t.Errorf("Head data length = %d, want 0", len(head.Data))
	}

	full, err := blobs.Get(ctx, "scan-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(full.Data) != "pdf" {
		t.Errorf("Get data = %q, want pdf", full.Data)
	}
}

func TestCursorStoreUpsertGetDelete(t *testing.T) {
	ctx := context.Background()
	cursors := newMemCursorStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	if err := cursors.UpsertCursor(ctx, store.Cursor{Name: "sync", Position: "", UpdatedAt: now}); err != nil {
		t.Fatalf("UpsertCursor empty position: %v", err)
	}
	empty, err := cursors.GetCursor(ctx, "sync")
	if err != nil {
		t.Fatalf("GetCursor empty: %v", err)
	}
	if empty.Position != "" {
		t.Errorf("Position = %q, want empty", empty.Position)
	}

	updated := now.Add(time.Minute)
	if err := cursors.UpsertCursor(ctx, store.Cursor{Name: "sync", Position: "99", UpdatedAt: updated}); err != nil {
		t.Fatalf("UpsertCursor non-empty position: %v", err)
	}
	cursor, err := cursors.GetCursor(ctx, "sync")
	if err != nil {
		t.Fatalf("GetCursor: %v", err)
	}
	if cursor.Position != "99" {
		t.Errorf("Position = %q, want 99", cursor.Position)
	}

	if err := cursors.DeleteCursor(ctx, "sync"); err != nil {
		t.Fatalf("DeleteCursor: %v", err)
	}
	if _, err := cursors.GetCursor(ctx, "sync"); err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestConflictStoreAppendListResolve(t *testing.T) {
	ctx := context.Background()
	conflicts := newMemConflictStore()
	created := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	resolved := created.Add(time.Hour)

	record := store.ConflictRecord{
		ID:              "conflict-1",
		ResourceType:    "Patient",
		ResourceID:      "pat-1",
		LocalVersionID:  "1",
		RemoteVersionID: "2",
		Reason:          "version mismatch",
		CreatedAt:       created,
	}
	if err := conflicts.Append(ctx, record); err != nil {
		t.Fatalf("Append: %v", err)
	}

	list, err := conflicts.List(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != "conflict-1" {
		t.Fatalf("List = %+v, want one conflict", list)
	}

	if err := conflicts.Resolve(ctx, "conflict-1", resolved); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	list, err = conflicts.List(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("List after resolve: %v", err)
	}
	if list[0].ResolvedAt == nil || !list[0].ResolvedAt.Equal(resolved) {
		t.Fatalf("ResolvedAt = %v, want %v", list[0].ResolvedAt, resolved)
	}
}

func TestAnalyticsStoreAppendAndQueryPrepared(t *testing.T) {
	ctx := context.Background()
	analytics := newMemAnalyticsStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	for _, id := range []string{"a-1", "a-2"} {
		if err := analytics.Append(ctx, store.AnalyticsEvent{
			ID:         id,
			Name:       "resource.write",
			Timestamp:  now,
			Dimensions: map[string]string{"resourceType": "Patient"},
			Values:     map[string]float64{"latencyMs": 12.5},
		}); err != nil {
			t.Fatalf("Append %s: %v", id, err)
		}
	}
	if err := analytics.Append(ctx, store.AnalyticsEvent{
		ID:        "a-3",
		Name:      "search.execute",
		Timestamp: now,
	}); err != nil {
		t.Fatalf("Append search event: %v", err)
	}

	events, err := analytics.QueryPrepared(ctx, store.PreparedQuery{Name: "by-name"}, map[string]string{
		"name": "resource.write",
	})
	if err != nil {
		t.Fatalf("QueryPrepared: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events length = %d, want 2", len(events))
	}
}

func TestAuditStoreAppendAndList(t *testing.T) {
	ctx := context.Background()
	audit := newMemAuditStore()
	early := time.Date(2024, 6, 1, 9, 0, 0, 0, time.UTC)
	later := time.Date(2024, 6, 1, 11, 0, 0, 0, time.UTC)

	for _, record := range []store.AuditRecord{
		{
			ID:           "audit-1",
			Timestamp:    early,
			Actor:        "nurse-1",
			Action:       "read",
			ResourceType: "Patient",
			ResourceID:   "pat-1",
			Outcome:      "success",
		},
		{
			ID:           "audit-2",
			Timestamp:    later,
			Actor:        "nurse-1",
			Action:       "update",
			ResourceType: "Patient",
			ResourceID:   "pat-1",
			Outcome:      "success",
		},
	} {
		if err := audit.Append(ctx, record); err != nil {
			t.Fatalf("Append %s: %v", record.ID, err)
		}
	}

	records, err := audit.List(ctx, store.AuditQuery{
		ResourceType: "Patient",
		ResourceID:   "pat-1",
		Actor:        "nurse-1",
		After:        early.Add(30 * time.Minute),
		Limit:        1,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 1 || records[0].ID != "audit-2" {
		t.Fatalf("List = %+v, want audit-2 only", records)
	}
}

func TestWriteSessionProviderCommitAndRollback(t *testing.T) {
	ctx := context.Background()
	provider := newMemWriteSessionProvider()

	session, err := provider.BeginWrite(ctx)
	if err != nil {
		t.Fatalf("BeginWrite: %v", err)
	}
	if session.ResourceStore() == nil || session.HistoryStore() == nil || session.SearchStore() == nil || session.EventStore() == nil {
		t.Fatal("expected non-nil store accessors")
	}
	if err := session.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	session, err = provider.BeginWrite(ctx)
	if err != nil {
		t.Fatalf("BeginWrite: %v", err)
	}
	if err := session.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
}

func TestIDRegistryEntryZeroValueJSON(t *testing.T) {
	var entry store.IDRegistryEntry
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal zero value: %v", err)
	}

	var decoded store.IDRegistryEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal zero value: %v", err)
	}
	if decoded.ID != "" {
		t.Errorf("ID = %q, want empty", decoded.ID)
	}
}

func TestIDRegistryStoreReserveRegisterCheck(t *testing.T) {
	ctx := context.Background()
	registry := newMemIDRegistryStore()

	registered, err := registry.Check(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("Check before register: %v", err)
	}
	if registered {
		t.Fatal("expected id to be unregistered")
	}

	if err := registry.Reserve(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := registry.Reserve(ctx, "Patient", "pat-1"); err == nil {
		t.Fatal("expected error for duplicate reserve")
	}

	if err := registry.Register(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	registered, err = registry.Check(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("Check after register: %v", err)
	}
	if !registered {
		t.Fatal("expected id to be registered")
	}
}
