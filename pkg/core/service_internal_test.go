package core

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestNewResourceServiceSetsOutbox(t *testing.T) {
	ctx := context.Background()
	recorder := &recordingEventStore{}
	svc, err := NewResourceService(ResourceServiceConfig{
		Resources: stubResourceStore{},
		History:   stubHistoryStore{},
		Sessions:  stubSessionProvider{session: stubWriteSession{events: recorder}},
		Outbox:    &hasync.EventStoreOutbox{},
	})
	if err != nil {
		t.Fatalf("NewResourceService: %v", err)
	}
	if svc.outbox == nil {
		t.Fatal("expected outbox to be configured")
	}

	_, err = svc.Create(ctx, &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "pat-1",
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"}`),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if recorder.count != 1 {
		t.Fatalf("event append count = %d, want 1", recorder.count)
	}
}

type recordingEventStore struct {
	count int
}

func (r *recordingEventStore) Append(context.Context, store.ResourceEvent) (store.ResourceEvent, error) {
	r.count++
	return store.ResourceEvent{}, nil
}

func (r *recordingEventStore) ReadSince(context.Context, int64, int) ([]store.ResourceEvent, error) {
	return nil, nil
}

func (r *recordingEventStore) LatestForResource(context.Context, string, string) (*store.ResourceEvent, error) {
	return nil, nil
}

type stubWriteSession struct {
	events *recordingEventStore
}

func (s stubWriteSession) ResourceStore() store.ResourceStore { return stubResourceStore{} }
func (s stubWriteSession) HistoryStore() store.HistoryStore   { return stubHistoryStore{} }
func (s stubWriteSession) SearchStore() store.SearchStore     { return stubSearchStore{} }
func (s stubWriteSession) EventStore() store.EventStore       { return s.events }
func (s stubWriteSession) Commit(context.Context) error       { return nil }
func (s stubWriteSession) Rollback(context.Context) error     { return nil }

type stubSessionProvider struct {
	session stubWriteSession
}

func (p stubSessionProvider) BeginWrite(context.Context) (store.WriteSession, error) {
	return p.session, nil
}

type stubResourceStore struct{}

func (stubResourceStore) Create(context.Context, *types.ResourceEnvelope) error { return nil }
func (stubResourceStore) Read(context.Context, string, string) (*types.ResourceEnvelope, error) {
	return nil, nil
}
func (stubResourceStore) Update(context.Context, *types.ResourceEnvelope) error { return nil }
func (stubResourceStore) Delete(context.Context, string, string) error          { return nil }
func (stubResourceStore) Exists(context.Context, string, string) (bool, error)  { return false, nil }
func (stubResourceStore) ListIDs(context.Context, string, int, int) ([]string, error) {
	return nil, nil
}

type stubHistoryStore struct{}

func (stubHistoryStore) AppendVersion(context.Context, store.ResourceVersion) error { return nil }
func (stubHistoryStore) GetHistory(context.Context, string, string) ([]store.ResourceVersion, error) {
	return nil, nil
}

type stubSearchStore struct{}

func (stubSearchStore) Index(context.Context, store.SearchIndexEntry) error { return nil }
func (stubSearchStore) RemoveIndex(context.Context, string, string) error   { return nil }
func (stubSearchStore) Lookup(context.Context, string, string) ([]string, error) {
	return nil, nil
}
func (stubSearchStore) QueryPrepared(context.Context, store.PreparedQuery, map[string]string) ([]string, error) {
	return nil, nil
}
