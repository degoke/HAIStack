package ai_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/degoke/haistack/pkg/ai"
	"github.com/degoke/haistack/pkg/store"
)

type memSessionService struct {
	mu       sync.Mutex
	sessions map[string]*store.AgentSession
	events   map[string][]store.SessionEvent
}

func sessionKey(tenant, app, user, id string) string {
	return tenant + "/" + app + "/" + user + "/" + id
}

func (m *memSessionService) CreateSession(_ context.Context, params store.CreateSessionParams) (*store.AgentSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := params.SessionID
	if id == "" {
		id = "sess-1"
	}
	key := sessionKey(params.TenantID, params.AppName, params.UserID, id)
	now := time.Now().UTC()
	rec := &store.AgentSession{
		ID: id, TenantID: params.TenantID, AppName: params.AppName, UserID: params.UserID,
		Subject: params.Subject, State: params.InitialState, CreatedAt: now, UpdatedAt: now,
	}
	m.sessions[key] = rec
	return rec, nil
}

func (m *memSessionService) GetSession(_ context.Context, params store.GetSessionParams) (*store.AgentSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := sessionKey(params.TenantID, params.AppName, params.UserID, params.SessionID)
	rec, ok := m.sessions[key]
	if !ok {
		return nil, store.ErrSessionNotFound
	}
	out := *rec
	out.Events = append([]store.SessionEvent(nil), m.events[key]...)
	return &out, nil
}

func (m *memSessionService) ListSessions(context.Context, store.ListSessionsParams) ([]store.SessionSummary, error) {
	return nil, nil
}

func (m *memSessionService) DeleteSession(context.Context, store.DeleteSessionParams) error {
	return nil
}

func (m *memSessionService) AppendEvent(_ context.Context, params store.AppendEventParams) (*store.SessionEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := sessionKey(params.TenantID, params.AppName, params.UserID, params.SessionID)
	m.events[key] = append(m.events[key], params.Event)
	return &params.Event, nil
}

func (m *memSessionService) GetUserState(context.Context, string, string, string) (map[string]any, error) {
	return map[string]any{}, nil
}

func (m *memSessionService) GetAppState(context.Context, string, string) (map[string]any, error) {
	return map[string]any{}, nil
}

func TestHarness_SessionServicePersistsEvents(t *testing.T) {
	h := newTestHarness(t, harnessOptions{})
	svc := &memSessionService{sessions: map[string]*store.AgentSession{}, events: map[string][]store.SessionEvent{}}
	model := &recordingChatModel{responses: []*ai.ChatResponse{
		{Content: "hello"},
		{Content: "welcome back"},
	}}
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor:       h.exec,
		Model:          model,
		Actor:          "user-1",
		TenantID:       "tenant-a",
		AppName:        "demo",
		SessionService: svc,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.SetConversationID("conv-1")
	_, err = harness.Chat(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}

	harness2, _ := ai.NewHarness(ai.HarnessConfig{
		Executor: h.exec, Model: model, Actor: "user-1", TenantID: "tenant-a",
		AppName: "demo", SessionService: svc,
	})
	harness2.SetConversationID("conv-1")
	_, err = harness2.Chat(context.Background(), "again")
	if err != nil {
		t.Fatal(err)
	}
	if len(harness2.Session().Messages) < 3 {
		t.Fatalf("messages = %d", len(harness2.Session().Messages))
	}
}

func TestHarness_SessionServiceCreateOnMissing(t *testing.T) {
	svc := &memSessionService{sessions: map[string]*store.AgentSession{}, events: map[string][]store.SessionEvent{}}
	h := newTestHarness(t, harnessOptions{})
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor: h.exec, Model: &recordingChatModel{responses: []*ai.ChatResponse{{Content: "ok"}}},
		Actor: "u", TenantID: "t", SessionService: svc,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.SetConversationID("new-session")
	_, err = harness.Chat(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.GetSession(context.Background(), store.GetSessionParams{
		TenantID: "t", AppName: ai.DefaultHarnessAppName, UserID: "u", SessionID: "new-session",
	})
	if errors.Is(err, store.ErrSessionNotFound) {
		t.Fatal("expected session to be created")
	}
}
