// Command ai-harness-host-approval demonstrates policy approval on host CommitWritePlan.
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
		fmt.Fprintf(os.Stderr, "ai-harness-host-approval: %v\n", err)
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

	h, err := ai.NewHarness(aiharnessdemo.HarnessConfig(exec, sessionSvc, &noopModel{}, true, func(plan ai.ResourceWritePlan) error {
		fmt.Printf("Host confirmed plan (%d entries)\n", len(plan.Entries))
		return nil
	}))
	if err != nil {
		return err
	}

	plan := ai.ResourceWritePlan{
		Entries: []ai.ResourceWriteDraft{{
			Operation:    ai.WriteOperationCreate,
			ResourceType: "Patient",
			Fields:       map[string]any{"gender": "unknown"},
		}},
	}
	pending, err := h.CommitWritePlan(ctx, plan)
	if err != nil {
		return err
	}
	if pending == nil || !pending.ApprovalRequired {
		return fmt.Errorf("expected approval-required, got %#v", pending)
	}
	fmt.Printf("CommitWritePlan pending: tool=%s token=%s…\n", pending.ToolName, truncate(pending.ApprovalToken, 12))

	if err := store.Approve(pending.ApprovalToken); err != nil {
		return err
	}
	_, err = h.CommitWritePlanWithOptions(ctx, plan, ai.CommitWriteOptions{
		ApprovalToken: pending.ApprovalToken,
	})
	if err != nil {
		return err
	}
	fmt.Println("Host resumed CommitWritePlan after policy approval.")
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

type noopModel struct{}

func (noopModel) Name() string { return "noop" }

func (noopModel) Chat(context.Context, ai.ChatRequest) (*ai.ChatResponse, error) {
	return &ai.ChatResponse{Content: "noop"}, nil
}
