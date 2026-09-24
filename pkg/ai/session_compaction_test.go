package ai_test

import (
	"context"
	"strings"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
	"github.com/degoke/haistack/pkg/store"
)

func TestEventsForModelContextUsesLatestCheckpoint(t *testing.T) {
	events := []store.SessionEvent{
		{ID: "1", Author: store.SessionAuthorUser, Content: "old user"},
		{ID: "2", Author: store.SessionAuthorModel, Content: "old reply"},
		{ID: "3", Author: store.SessionAuthorCompaction, Content: "summary one"},
		{ID: "4", Author: store.SessionAuthorUser, Content: "new user"},
	}
	summary, active := ai.EventsForModelContext(events)
	if summary != "summary one" || len(active) != 1 || active[0].Content != "new user" {
		t.Fatalf("summary=%q active=%+v", summary, active)
	}
	msgs := ai.ChatMessagesFromSessionEvents(events)
	if len(msgs) != 2 {
		t.Fatalf("msgs = %+v", msgs)
	}
	if msgs[0].Role != ai.ChatRoleSystem || !strings.Contains(msgs[0].Content, "summary one") {
		t.Fatalf("system = %+v", msgs[0])
	}
}

func TestHarness_SessionCompactionAppendsCheckpoint(t *testing.T) {
	svc := &memSessionService{sessions: map[string]*store.AgentSession{}, events: map[string][]store.SessionEvent{}}
	h := newTestHarness(t, harnessOptions{})
	var big strings.Builder
	for i := 0; i < 40; i++ {
		big.WriteString("patient context line ")
	}
	summaryModel := &recordingChatModel{responses: []*ai.ChatResponse{
		{Content: "compact summary of prior turns"},
		{Content: "answer"},
	}}
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor: h.exec,
		Model:    summaryModel,
		Actor:    "u",
		TenantID: "t",
		SessionService: svc,
		SessionCompaction: ai.SessionCompactionConfig{
			MaxContextTokens:   80,
			RetainRecentEvents: 2,
			MinEventsToCompact: 4,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.SetConversationID("compact-session")
	// Seed history directly in store.
	ctx := context.Background()
	_, err = svc.CreateSession(ctx, store.CreateSessionParams{
		TenantID: "t", AppName: ai.DefaultHarnessAppName, UserID: "u", SessionID: "compact-session",
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		author := store.SessionAuthorUser
		content := big.String()
		if i%2 == 1 {
			author = store.SessionAuthorModel
			content = "ack"
		}
		_, err = svc.AppendEvent(ctx, store.AppendEventParams{
			TenantID: "t", AppName: ai.DefaultHarnessAppName, UserID: "u", SessionID: "compact-session",
			Event: store.SessionEvent{Author: author, Content: content},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = harness.Chat(ctx, "next")
	if err != nil {
		t.Fatal(err)
	}
	rec, err := svc.GetSession(ctx, store.GetSessionParams{
		TenantID: "t", AppName: ai.DefaultHarnessAppName, UserID: "u", SessionID: "compact-session",
	})
	if err != nil {
		t.Fatal(err)
	}
	var checkpoints int
	for _, ev := range rec.Events {
		if ev.Author == store.SessionAuthorCompaction {
			checkpoints++
		}
	}
	if checkpoints < 1 {
		t.Fatalf("expected compaction checkpoint, events=%d", len(rec.Events))
	}
	msgs := harness.Session().Messages
	if len(msgs) == 0 {
		t.Fatal("expected model context messages")
	}
	if !strings.Contains(msgs[0].Content, "compact summary") {
		t.Fatalf("context should start from checkpoint: %+v", msgs[0])
	}
}
