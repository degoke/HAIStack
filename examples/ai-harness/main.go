// Command ai-harness demonstrates Harness.Chat with a scripted ChatModel (no HTTP LLM).
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/degoke/haistack/examples/internal/appkit"
	"github.com/degoke/haistack/pkg/ai"
	"github.com/degoke/haistack/pkg/store"
	"github.com/degoke/haistack/pkg/validate"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ai-harness: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "haistack-ai-harness-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	stack, err := appkit.NewSQLiteStack(ctx, filepath.Join(tempDir, "ai.db"), "Patient")
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	patient, err := appkit.EnvelopeFromJSON("Patient", appkit.PatientJSON("Ava", "Chen", "+1-555-0142"))
	if err != nil {
		return err
	}
	created, err := stack.ResourceService.Create(ctx, patient)
	if err != nil {
		return err
	}

	approvalStore := ai.NewMemoryApprovalStore()
	exec, err := newExampleExecutor(stack, approvalStore)
	if err != nil {
		return err
	}

	const tenantID = "demo-tenant"
	sessionSvc := stack.DB.SessionService(tenantID)

	model := &scriptedChatModel{patientID: created.ID}
	h, err := ai.NewHarness(exampleHarnessConfig(exec, sessionSvc, tenantID, model, true))
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

	genderPlan := ai.ResourceWritePlan{
		Entries: []ai.ResourceWriteDraft{{
			Operation:    ai.WriteOperationUpdate,
			ResourceType: "Patient",
			ID:           created.ID,
			Patches:      map[string]any{"gender": "female"},
		}},
	}
	_, err = h.CommitWritePlan(ctx, genderPlan)
	if err != nil {
		return err
	}

	// Policy approval on host CommitWritePlan (same plan + ApprovalToken retry).
	createPlan := ai.ResourceWritePlan{
		Entries: []ai.ResourceWriteDraft{{
			Operation:    ai.WriteOperationCreate,
			ResourceType: "Patient",
			Fields:       map[string]any{"gender": "unknown"},
		}},
	}
	pending, err := h.CommitWritePlan(ctx, createPlan)
	if err != nil {
		return err
	}
	if pending != nil && pending.ApprovalRequired {
		fmt.Printf("Host policy approval: tool=%s token=%s…\n", pending.ToolName, truncate(pending.ApprovalToken, 8))
		if err := approvalStore.Approve(pending.ApprovalToken); err != nil {
			return err
		}
		_, err = h.CommitWritePlanWithOptions(ctx, createPlan, ai.CommitWriteOptions{
			ApprovalToken: pending.ApprovalToken,
		})
		if err != nil {
			return err
		}
		fmt.Println("Host resumed write after policy approval.")
	}

	h2, err := ai.NewHarness(exampleHarnessConfig(exec, sessionSvc, tenantID, &scriptedChatModel{patientID: created.ID}, true))
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
	if len(res.GroundingWarnings) > 0 {
		fmt.Println("Grounding warnings:")
		for _, w := range res.GroundingWarnings {
			fmt.Println(" -", w)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func newExampleExecutor(stack *appkit.SQLiteStack, approvalStore ai.ApprovalStore) (*ai.Executor, error) {
	policy := ai.NewAllowListPolicy()
	policy.Read["Patient"] = ai.ReadTypePolicy{AllowedFields: []string{"name", "gender"}}
	policy.Search["Patient"] = ai.SearchTypePolicy{
		AllowedParams: []string{"name"},
		AllowedFields: []string{"name"},
		MaxCount:      10,
	}
	policy.Write["Patient"] = ai.WriteTypePolicy{
		CreateFields:   []string{"name", "gender"},
		UpdateFields:   []string{"gender", "name"},
		CreateApproval: approvalStore != nil,
	}

	validator, err := validate.NewEngine(validate.Config{})
	if err != nil {
		return nil, err
	}
	atomic := true
	return ai.NewExecutor(ai.Config{
		Resources:                stack.DB.ResourceStore(),
		Search:                   stack.SearchService,
		Core:                     stack.ResourceService,
		Policy:                   policy,
		Validator:                validator,
		RequireValidatorOnWrites: true,
		Audit:                    &ai.AuditStoreAdapter{Store: stack.DB.AuditStore()},
		AuditRequired:            true,
		ApprovalStore:            approvalStore,
		AIAttribution: ai.AIAttributionConfig{
			Enabled:          true,
			AgentDisplay:     "ai-harness-example",
			AtomicProvenance: &atomic,
		},
	})
}

func exampleHarnessConfig(exec *ai.Executor, sessionSvc store.SessionService, tenantID string, model ai.ChatModel, blockDirectWrites bool) ai.HarnessConfig {
	return ai.HarnessConfig{
		Executor:                     exec,
		Model:                        model,
		Actor:                        "demo-agent",
		TenantID:                     tenantID,
		AppName:                      "ai-harness-example",
		SessionService:               sessionSvc,
		SystemPrompt:                 "Use FHIR tools for facts. Propose writes with propose_write_resource or propose_write_plan; the host commits.",
		ToolContextFormat:            ai.ToolContextMarkdown,
		BlockDirectWriteTools:        blockDirectWrites,
		EnableProposeWriteHelper:     true,
		EnableProposeWritePlanHelper: true,
		Grounding:                    ai.GroundingConfig{Mode: ai.GroundingStandard},
		RequireCommitConfirmation:    true,
		CommitWritePlanConfirm: func(_ context.Context, plan ai.ResourceWritePlan) error {
			fmt.Printf("Host confirmed write plan (%d entries, bundleType=%s)\n", len(plan.Entries), planBundleTypeLabel(plan))
			return nil
		},
	}
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

// scriptedChatModel fakes a tool-calling LLM for the example (read → final answer).
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
