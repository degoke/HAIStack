// Package aiharnessdemo shares wiring for pkg/ai harness examples.
package aiharnessdemo

import (
	"context"
	"os"
	"path/filepath"

	"github.com/degoke/haistack/examples/internal/appkit"
	"github.com/degoke/haistack/pkg/ai"
	"github.com/degoke/haistack/pkg/store"
	"github.com/degoke/haistack/pkg/validate"
)

const DemoTenantID = "demo-tenant"

// DemoPatient seeds one Patient and returns its id.
func DemoPatient(ctx context.Context) (stack *appkit.SQLiteStack, patientID string, cleanup func(), err error) {
	tempDir, err := os.MkdirTemp("", "haistack-ai-harness-*")
	if err != nil {
		return nil, "", nil, err
	}
	removeTemp := func() { _ = os.RemoveAll(tempDir) }
	stack, err = appkit.NewSQLiteStack(ctx, filepath.Join(tempDir, "ai.db"), "Patient")
	if err != nil {
		removeTemp()
		return nil, "", nil, err
	}
	patient, err := appkit.EnvelopeFromJSON("Patient", appkit.PatientJSON("Ava", "Chen", "+1-555-0142"))
	if err != nil {
		_ = stack.Close()
		removeTemp()
		return nil, "", nil, err
	}
	created, err := stack.ResourceService.Create(ctx, patient)
	if err != nil {
		_ = stack.Close()
		removeTemp()
		return nil, "", nil, err
	}
	return stack, created.ID, func() {
		_ = stack.Close()
		removeTemp()
	}, nil
}

// NewExecutor builds a governed AI executor for harness examples.
func NewExecutor(stack *appkit.SQLiteStack, approvalStore ai.ApprovalStore, requireCreateApproval bool) (*ai.Executor, error) {
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
		CreateApproval: requireCreateApproval,
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

// HarnessConfig returns a standard harness wiring for examples.
func HarnessConfig(
	exec *ai.Executor,
	sessionSvc store.SessionService,
	model ai.ChatModel,
	blockDirectWrites bool,
	onConfirm func(ai.ResourceWritePlan) error,
) ai.HarnessConfig {
	cfg := ai.HarnessConfig{
		Executor:                     exec,
		Model:                        model,
		Actor:                        "demo-agent",
		TenantID:                     DemoTenantID,
		AppName:                      "ai-harness-example",
		SessionService:               sessionSvc,
		SystemPrompt:                 "Use FHIR tools for facts. Propose writes with propose_write_resource or propose_write_plan; the host commits.",
		ToolContextFormat:            ai.ToolContextMarkdown,
		BlockDirectWriteTools:        blockDirectWrites,
		EnableProposeWriteHelper:     true,
		EnableProposeWritePlanHelper: true,
		Grounding:                    ai.GroundingConfig{Mode: ai.GroundingStandard},
		RequireCommitConfirmation:    onConfirm != nil,
	}
	if onConfirm != nil {
		cfg.CommitWritePlanConfirm = func(ctx context.Context, plan ai.ResourceWritePlan) error {
			return onConfirm(plan)
		}
	}
	return cfg
}
