// Command ai-harness demonstrates Harness.Chat, propose_write_plan, and host CommitWritePlan.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/degoke/haistack/examples/internal/aiharnessdemo"
	"github.com/degoke/haistack/pkg/ai"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ai-harness: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	stack, patientID, cleanup, err := aiharnessdemo.DemoPatient(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	exec, err := aiharnessdemo.NewExecutor(stack, nil, false)
	if err != nil {
		return err
	}
	sessionSvc := stack.DB.SessionService(aiharnessdemo.DemoTenantID)

	model := &scriptedChatModel{patientID: patientID}
	h, err := ai.NewHarness(aiharnessdemo.HarnessConfig(exec, sessionSvc, model, true, func(plan ai.ResourceWritePlan) error {
		fmt.Printf("Host confirmed write plan (%d entries, bundleType=%s)\n", len(plan.Entries), planBundleTypeLabel(plan))
		return nil
	}))
	if err != nil {
		return err
	}
	h.SetConversationID("demo-session-1")

	res, err := h.Chat(ctx, "Who is our demo patient?")
	if err != nil {
		return err
	}

	planRes, err := h.Chat(ctx, "Propose a write plan to set gender to female.")
	if err != nil {
		return err
	}
	fmt.Println("Model proposed write plan (tool message in session).")

	_, err = h.CommitWritePlan(ctx, ai.ResourceWritePlan{
		Entries: []ai.ResourceWriteDraft{{
			Operation:    ai.WriteOperationUpdate,
			ResourceType: "Patient",
			ID:           patientID,
			Patches:      map[string]any{"gender": "female"},
		}},
	})
	if err != nil {
		return err
	}

	h2, err := ai.NewHarness(aiharnessdemo.HarnessConfig(exec, sessionSvc, &scriptedChatModel{patientID: patientID}, true, nil))
	if err != nil {
		return err
	}
	h2.SetConversationID("demo-session-1")
	res2, err := h2.Chat(ctx, "Summarize what you found earlier.")
	if err != nil {
		return err
	}

	fmt.Println("Harness answer:")
	fmt.Println(res.Answer)
	fmt.Println("Follow-up (session resumed):")
	fmt.Println(res2.Answer)
	if len(planRes.ToolResults) > 0 {
		fmt.Println("Write-plan tool calls:", len(planRes.ToolResults))
	}
	fmt.Println("Citations:")
	for _, c := range res.Citations {
		fmt.Println(" -", c.Ref)
	}
	return nil
}

func planBundleTypeLabel(plan ai.ResourceWritePlan) string {
	if strings.TrimSpace(plan.BundleType) != "" {
		return plan.BundleType
	}
	for _, e := range plan.Entries {
		if e.Operation == ai.WriteOperationRead {
			return "batch"
		}
	}
	return "transaction"
}

type scriptedChatModel struct {
	patientID    string
	step         int
	proposedPlan bool
}

func (m *scriptedChatModel) Name() string { return "scripted" }

func (m *scriptedChatModel) Chat(_ context.Context, req ai.ChatRequest) (*ai.ChatResponse, error) {
	m.step++
	userMsg := lastUserMessage(req.Messages)

	if strings.Contains(strings.ToLower(userMsg), "summarize") {
		return &ai.ChatResponse{Content: "Earlier I loaded the demo patient from FHIR (see prior tool result)."}, nil
	}
	if strings.Contains(strings.ToLower(userMsg), "write plan") && !m.proposedPlan {
		m.proposedPlan = true
		return &ai.ChatResponse{
			ToolCalls: []ai.ChatToolCall{{
				ID:   "call_plan",
				Name: ai.ToolProposeWritePlan,
				Arguments: fmt.Sprintf(
					`{"entries":[{"operation":"update","resourceType":"Patient","id":"%s","patches":{"gender":"female"}}]}`,
					m.patientID,
				),
			}},
		}, nil
	}
	if m.step == 1 {
		return &ai.ChatResponse{
			ToolCalls: []ai.ChatToolCall{{
				ID:   "call_1",
				Name: ai.ToolReadFhirResource,
				Arguments: fmt.Sprintf(
					`{"resourceType":"Patient","id":"%s"}`, m.patientID,
				),
			}},
		}, nil
	}
	return &ai.ChatResponse{Content: "The demo patient record was loaded from FHIR."}, nil
}

func lastUserMessage(messages []ai.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == ai.ChatRoleUser {
			return messages[i].Content
		}
	}
	return ""
}
