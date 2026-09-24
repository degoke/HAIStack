package ai_test

import (
	"context"
	"errors"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
	"github.com/degoke/haistack/pkg/store"
)

type errOnceChatModel struct {
	calls int
	ok    *ai.ChatResponse
}

func (m *errOnceChatModel) Name() string { return "err-once" }

func (m *errOnceChatModel) Chat(_ context.Context, _ ai.ChatRequest) (*ai.ChatResponse, error) {
	m.calls++
	if m.calls == 1 {
		return nil, errors.New("model unavailable")
	}
	return m.ok, nil
}

func TestHarness_InvocationIDSkipsDuplicateUserEvent(t *testing.T) {
	svc := &memSessionService{sessions: map[string]*store.AgentSession{}, events: map[string][]store.SessionEvent{}}
	h := newTestHarness(t, harnessOptions{})
	model := &errOnceChatModel{ok: &ai.ChatResponse{Content: "recovered"}}
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor: h.exec, Model: model, Actor: "u", TenantID: "t", SessionService: svc,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.SetConversationID("inv-session")
	const inv = "inv-retry-1"
	_, err = harness.ChatWithOptions(context.Background(), ai.ChatOptions{
		UserMessage: "hello", InvocationID: inv,
	})
	if err == nil {
		t.Fatal("expected model error")
	}
	rec, err := svc.GetSession(context.Background(), store.GetSessionParams{
		TenantID: "t", AppName: ai.DefaultHarnessAppName, UserID: "u", SessionID: "inv-session",
	})
	if err != nil {
		t.Fatal(err)
	}
	userEvents := 0
	for _, ev := range rec.Events {
		if ev.InvocationID == inv && ev.Author == store.SessionAuthorUser {
			userEvents++
		}
	}
	if userEvents != 1 {
		t.Fatalf("user events for invocation = %d", userEvents)
	}

	res, err := harness.ChatWithOptions(context.Background(), ai.ChatOptions{
		UserMessage: "hello", InvocationID: inv,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer != "recovered" {
		t.Fatalf("answer = %q", res.Answer)
	}
	got, err := svc.GetSession(context.Background(), store.GetSessionParams{
		TenantID: "t", AppName: ai.DefaultHarnessAppName, UserID: "u", SessionID: "inv-session",
	})
	if err != nil {
		t.Fatal(err)
	}
	userEvents = 0
	for _, ev := range got.Events {
		if ev.InvocationID == inv && ev.Author == store.SessionAuthorUser {
			userEvents++
		}
	}
	if userEvents != 1 {
		t.Fatalf("after retry user events = %d", userEvents)
	}
}

func TestHarness_InvocationIDReturnsCompletedTurn(t *testing.T) {
	svc := &memSessionService{sessions: map[string]*store.AgentSession{}, events: map[string][]store.SessionEvent{}}
	h := newTestHarness(t, harnessOptions{})
	model := &recordingChatModel{responses: []*ai.ChatResponse{{Content: "done once"}}}
	harness, _ := ai.NewHarness(ai.HarnessConfig{
		Executor: h.exec, Model: model, Actor: "u", TenantID: "t", SessionService: svc,
	})
	harness.SetConversationID("done-session")
	const inv = "inv-done"
	res1, err := harness.ChatWithOptions(context.Background(), ai.ChatOptions{
		UserMessage: "hi", InvocationID: inv,
	})
	if err != nil {
		t.Fatal(err)
	}
	callsAfterFirst := len(model.requests)
	res2, err := harness.ChatWithOptions(context.Background(), ai.ChatOptions{
		UserMessage: "hi", InvocationID: inv,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Answer != res1.Answer {
		t.Fatalf("answers differ: %q vs %q", res1.Answer, res2.Answer)
	}
	if len(model.requests) != callsAfterFirst {
		t.Fatalf("expected no extra model calls, got %d", len(model.requests))
	}
}
