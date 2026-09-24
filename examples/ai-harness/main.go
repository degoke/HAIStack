// Command ai-harness demonstrates Harness.Chat with a scripted ChatModel (no HTTP LLM).
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/degoke/haistack/examples/internal/appkit"
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

	policy := ai.NewAllowListPolicy()
	policy.Read["Patient"] = ai.ReadTypePolicy{AllowedFields: []string{"name", "gender"}}
	policy.Search["Patient"] = ai.SearchTypePolicy{
		AllowedParams: []string{"name"},
		AllowedFields: []string{"name"},
		MaxCount:      10,
	}

	exec, err := ai.NewExecutor(ai.Config{
		Resources:     stack.DB.ResourceStore(),
		Search:        stack.SearchService,
		Core:          stack.ResourceService,
		Policy:        policy,
		Audit:         &ai.AuditStoreAdapter{Store: stack.DB.AuditStore()},
		AuditRequired: true,
	})
	if err != nil {
		return err
	}

	const tenantID = "demo-tenant"
	sessionSvc := stack.DB.SessionService(tenantID)

	model := &scriptedChatModel{patientID: created.ID}
	cfg := ai.HarnessConfig{
		Executor:                  exec,
		Model:                     model,
		Actor:                     "demo-agent",
		TenantID:                  tenantID,
		AppName:                   "ai-harness-example",
		SessionService:            sessionSvc,
		SystemPrompt:              "Use FHIR tools for facts.",
		ToolContextFormat:         ai.ToolContextMarkdown,
		BlockDirectWriteTools:     true,
		EnableProposeWriteHelper: true,
	}
	h, err := ai.NewHarness(cfg)
	if err != nil {
		return err
	}
	h.SetConversationID("demo-session-1")

	res, err := h.Chat(ctx, "Who is our demo patient?")
	if err != nil {
		return err
	}

	// Resume the same ADK-style session in a fresh harness (events reloaded from SQLite).
	h2, err := ai.NewHarness(ai.HarnessConfig{
		Executor:                  exec,
		Model:                     &scriptedChatModel{patientID: created.ID},
		Actor:                     "demo-agent",
		TenantID:                  tenantID,
		AppName:                   "ai-harness-example",
		SessionService:            sessionSvc,
		SystemPrompt:              "Use FHIR tools for facts.",
		ToolContextFormat:         ai.ToolContextMarkdown,
		BlockDirectWriteTools:     true,
		EnableProposeWriteHelper: true,
	})
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
	fmt.Println("Citations:")
	for _, c := range res.Citations {
		fmt.Println(" -", c.Ref)
	}
	return nil
}

// scriptedChatModel fakes a tool-calling LLM for the example (read → final answer).
type scriptedChatModel struct {
	patientID string
	step      int
}

func (m *scriptedChatModel) Name() string { return "scripted" }

func (m *scriptedChatModel) Chat(_ context.Context, req ai.ChatRequest) (*ai.ChatResponse, error) {
	m.step++
	// After session restore, prior user/assistant/tool messages are already in context.
	if len(req.Messages) > 2 && m.step == 1 {
		return &ai.ChatResponse{Content: "Earlier I loaded the demo patient from FHIR (see prior tool result)."}, nil
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
