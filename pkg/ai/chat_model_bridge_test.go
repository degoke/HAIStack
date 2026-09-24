package ai_test

import (
	"context"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
)

type stubModelAdapter struct {
	last ai.ModelRequest
}

func (s *stubModelAdapter) Name() string { return "stub" }

func (s *stubModelAdapter) Invoke(_ context.Context, req ai.ModelRequest) (*ai.ModelResponse, error) {
	s.last = req
	return &ai.ModelResponse{Adapter: "stub", Content: "answer"}, nil
}

func TestModelAdapterChatModel_FlattensTranscript(t *testing.T) {
	stub := &stubModelAdapter{}
	bridge, err := ai.NewModelAdapterChatModel(stub, "local")
	if err != nil {
		t.Fatal(err)
	}
	_, err = bridge.Chat(context.Background(), ai.ChatRequest{
		SystemPrompt: "be safe",
		Messages: []ai.ChatMessage{
			{Role: ai.ChatRoleUser, Content: "first"},
			{Role: ai.ChatRoleAssistant, Content: "hi"},
			{Role: ai.ChatRoleUser, Content: "second"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stub.last.Prompt != "second" {
		t.Fatalf("prompt = %q", stub.last.Prompt)
	}
	if stub.last.Context == "" {
		t.Fatal("expected context transcript")
	}
}
