package ai_test

import (
	"context"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
	"github.com/degoke/haistack/pkg/store"
)

type memConversationStore struct {
	records map[string]store.ConversationRecord
}

func (m *memConversationStore) Get(_ context.Context, id string) (*store.ConversationRecord, error) {
	rec, ok := m.records[id]
	if !ok {
		return nil, store.ErrConversationNotFound
	}
	return &rec, nil
}

func (m *memConversationStore) Put(_ context.Context, record store.ConversationRecord) error {
	m.records[record.ID] = record
	return nil
}

func (m *memConversationStore) Delete(_ context.Context, id string) error {
	if _, ok := m.records[id]; !ok {
		return store.ErrConversationNotFound
	}
	delete(m.records, id)
	return nil
}

func (m *memConversationStore) List(_ context.Context, _ store.ConversationQuery) ([]store.ConversationRecord, error) {
	out := make([]store.ConversationRecord, 0, len(m.records))
	for _, rec := range m.records {
		out = append(out, rec)
	}
	return out, nil
}

func TestHarness_PersistsConversationAcrossChat(t *testing.T) {
	h := newTestHarness(t, harnessOptions{})
	store := &memConversationStore{records: map[string]store.ConversationRecord{}}
	model := &recordingChatModel{responses: []*ai.ChatResponse{
		{Content: "hi"},
		{Content: "again"},
	}}
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor:          h.exec,
		Model:             model,
		Actor:             "agent-1",
		TenantID:          "tenant-a",
		ConversationStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.SetConversationID("conv-1")
	_, err = harness.Chat(context.Background(), "one")
	if err != nil {
		t.Fatal(err)
	}
	harness2, err := ai.NewHarness(ai.HarnessConfig{
		Executor:          h.exec,
		Model:             model,
		Actor:             "agent-1",
		TenantID:          "tenant-a",
		ConversationStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness2.SetConversationID("conv-1")
	_, err = harness2.Chat(context.Background(), "two")
	if err != nil {
		t.Fatal(err)
	}
	if len(harness2.Session().Messages) < 3 {
		t.Fatalf("messages = %d", len(harness2.Session().Messages))
	}
}
