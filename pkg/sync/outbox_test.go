package sync_test

import (
	"context"
	"errors"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
)

func TestWithWriteSessionRoutesEventStoreOutboxToSession(t *testing.T) {
	session := &stubSession{events: &recordingEvents{}}
	outbox := hasync.WithWriteSession(&hasync.EventStoreOutbox{Events: &noopEvents{}}, session)

	if _, err := outbox.Append(context.Background(), store.ResourceEvent{ResourceType: "Patient", ID: "1"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if session.events.count != 1 {
		t.Fatalf("session append count = %d, want 1", session.events.count)
	}
}

func TestWithWriteSessionDelegatesCustomOutbox(t *testing.T) {
	custom := &failOutbox{}
	session := &stubSession{events: &recordingEvents{}}
	outbox := hasync.WithWriteSession(custom, session)

	_, err := outbox.Append(context.Background(), store.ResourceEvent{})
	if err == nil {
		t.Fatal("expected custom outbox error")
	}
	if session.events.count != 0 {
		t.Fatalf("session append count = %d, want 0", session.events.count)
	}
}

type recordingEvents struct {
	count int
}

func (r *recordingEvents) Append(context.Context, store.ResourceEvent) (store.ResourceEvent, error) {
	r.count++
	return store.ResourceEvent{}, nil
}

func (r *recordingEvents) ReadSince(context.Context, int64, int) ([]store.ResourceEvent, error) {
	return nil, nil
}

func (r *recordingEvents) LatestForResource(context.Context, string, string) (*store.ResourceEvent, error) {
	return nil, nil
}

type noopEvents struct{}

func (noopEvents) Append(context.Context, store.ResourceEvent) (store.ResourceEvent, error) {
	return store.ResourceEvent{}, nil
}

func (noopEvents) ReadSince(context.Context, int64, int) ([]store.ResourceEvent, error) {
	return nil, nil
}

func (noopEvents) LatestForResource(context.Context, string, string) (*store.ResourceEvent, error) {
	return nil, nil
}

type failOutbox struct{}

func (failOutbox) Append(context.Context, store.ResourceEvent) (store.ResourceEvent, error) {
	return store.ResourceEvent{}, errors.New("outbox failed")
}

type stubSession struct {
	events *recordingEvents
}

func (s *stubSession) ResourceStore() store.ResourceStore { return nil }
func (s *stubSession) HistoryStore() store.HistoryStore   { return nil }
func (s *stubSession) SearchStore() store.SearchStore     { return nil }
func (s *stubSession) EventStore() store.EventStore       { return s.events }
func (s *stubSession) Commit(context.Context) error       { return nil }
func (s *stubSession) Rollback(context.Context) error     { return nil }
