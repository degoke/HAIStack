package ai_test

import (
	"context"
	"strings"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
	"github.com/degoke/haistack/pkg/store"
)

func TestHarness_CompactionMetricsAndCustomSummarizer(t *testing.T) {
	svc := &memSessionService{sessions: map[string]*store.AgentSession{}, events: map[string][]store.SessionEvent{}}
	h := newTestHarness(t, harnessOptions{})
	var metricKinds []ai.CompactionMetricKind
	summarizer := func(_ context.Context, in ai.SessionCompactionSummarizeInput) (string, error) {
		return "custom summary", nil
	}
	model := &recordingChatModel{responses: []*ai.ChatResponse{{Content: "ok"}}}
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor: h.exec,
		Model:    model,
		Actor:    "u",
		TenantID: "t",
		SessionService: svc,
		SessionCompaction: ai.SessionCompactionConfig{
			MaxContextTokens:   10,
			RetainRecentEvents: 1,
			MinEventsToCompact: 2,
			Summarizer:         summarizer,
			OnMetric: func(ev ai.CompactionMetricEvent) {
				metricKinds = append(metricKinds, ev.Kind)
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.SetConversationID("m")
	ctx := context.Background()
	_, _ = svc.CreateSession(ctx, store.CreateSessionParams{
		TenantID: "t", AppName: ai.DefaultHarnessAppName, UserID: "u", SessionID: "m",
	})
	big := strings.Repeat("word ", 200)
	for i := 0; i < 4; i++ {
		_, _ = svc.AppendEvent(ctx, store.AppendEventParams{
			TenantID: "t", AppName: ai.DefaultHarnessAppName, UserID: "u", SessionID: "m",
			Event: store.SessionEvent{Author: store.SessionAuthorUser, Content: big},
		})
	}
	_, err = harness.Chat(ctx, "go")
	if err != nil {
		t.Fatal(err)
	}
	m := harness.CompactionMetrics()
	if m.Compactions < 1 {
		t.Fatalf("metrics = %+v kinds=%v", m, metricKinds)
	}
	rec, _ := svc.GetSession(ctx, store.GetSessionParams{
		TenantID: "t", AppName: ai.DefaultHarnessAppName, UserID: "u", SessionID: "m",
	})
	for _, ev := range rec.Events {
		if ev.Author == store.SessionAuthorCompaction && ev.Content != "custom summary" {
			t.Fatalf("summary = %q", ev.Content)
		}
	}
}
