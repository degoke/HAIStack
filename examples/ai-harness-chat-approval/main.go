// Command ai-harness-chat-approval demonstrates Chat policy approval and ExecuteHarnessTool resume.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/degoke/haistack/examples/internal/aiharnessdemo"
	"github.com/degoke/haistack/pkg/ai"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ai-harness-chat-approval: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	stack, _, cleanup, err := aiharnessdemo.DemoPatient(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	store := ai.NewMemoryApprovalStore()
	exec, err := aiharnessdemo.NewExecutor(stack, store, true)
	if err != nil {
		return err
	}
	sessionSvc := stack.DB.SessionService(aiharnessdemo.DemoTenantID)

	h, err := ai.NewHarness(aiharnessdemo.HarnessConfig(exec, sessionSvc, &createOnceModel{}, false, nil))
	if err != nil {
		return err
	}
	h.SetConversationID("chat-approval-demo")

	res, err := h.Chat(ctx, "Create a minimal patient record.")
	if err != nil {
		return err
	}
	if len(res.PendingApprovals) != 1 {
		return fmt.Errorf("expected 1 pending approval, got %d", len(res.PendingApprovals))
	}
	fmt.Println("Chat paused:", res.Answer)
	pending := res.PendingApprovals[0]
	fmt.Printf("Pending: tool=%s token=%s…\n", pending.ToolName, truncate(pending.Token, 12))

	if err := store.Approve(pending.Token); err != nil {
		return err
	}
	committed, err := h.ExecuteHarnessTool(ctx, ai.ToolRequest{
		ToolName:      pending.ToolName,
		Input:         pending.Input,
		ApprovalToken: pending.Token,
	})
	if err != nil {
		return err
	}
	if committed.ApprovalRequired {
		return fmt.Errorf("write still pending after approval")
	}
	fmt.Println("Host resumed write via ExecuteHarnessTool after policy approval.")
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

type createOnceModel struct {
	calls int
}

func (m *createOnceModel) Name() string { return "create-once" }

func (m *createOnceModel) Chat(_ context.Context, _ ai.ChatRequest) (*ai.ChatResponse, error) {
	m.calls++
	if m.calls > 1 {
		return nil, fmt.Errorf("model should not run again after policy approval pause")
	}
	return &ai.ChatResponse{
		ToolCalls: []ai.ChatToolCall{{
			ID:        "call_create",
			Name:      ai.ToolCreateFhirResource,
			Arguments: `{"resourceType":"Patient","fields":{"gender":"unknown"}}`,
		}},
	}, nil
}
