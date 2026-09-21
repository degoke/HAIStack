// Package aipipeline is the Track E reproducible FHIR → view → AI → audit demo.
package aipipeline

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/ai"
	"github.com/degoke/health-ai-stack/pkg/audit"
	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/degoke/health-ai-stack/pkg/view"
	"github.com/degoke/health-ai-stack/research/internal/memstore"
)

// Result is the pipeline outcome plus the exportable provenance bundle.
type Result struct {
	Bundle    ProvenanceBundle
	ViewRows  int
	DeniedErr string
}

// Run executes the reproducible pipeline with a fixed clock and seeded stub.
func Run(ctx context.Context) (*Result, error) {
	ds, err := LoadDataset()
	if err != nil {
		return nil, err
	}

	validator, err := validate.NewEngine(validate.Config{})
	if err != nil {
		return nil, fmt.Errorf("validate engine: %w", err)
	}
	inputs := make([]InputRef, 0, len(ds.Patients)+len(ds.Observations))
	resources := memstore.New()
	all := make([]*types.ResourceEnvelope, 0, len(ds.Patients)+len(ds.Observations))
	all = append(all, ds.Patients...)
	all = append(all, ds.Observations...)
	for _, env := range all {
		res, err := validator.Validate(ctx, env, validate.ValidateOptions{})
		if err != nil {
			return nil, fmt.Errorf("validate %s/%s: %w", env.ResourceType, env.ID, err)
		}
		if res != nil && !res.Valid {
			return nil, fmt.Errorf("validate %s/%s: invalid: %+v", env.ResourceType, env.ID, res.Issues)
		}
		if err := resources.Create(ctx, env); err != nil {
			return nil, err
		}
		inputs = append(inputs, InputRef{
			Ref:  env.ResourceType + "/" + env.ID,
			Hash: env.Hash,
		})
	}

	fp, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		return nil, fmt.Errorf("fhirpath: %w", err)
	}

	now := FixedNow
	auditStore := audit.NewMemoryStore()
	newID := sequentialID("audit")
	auditLogger := &audit.StoreAdapter{Store: auditStore, Now: now, NewID: newID}

	engine, err := newAuthEngine()
	if err != nil {
		return nil, err
	}
	resolve := func(_ context.Context, actor, _ string) (auth.Principal, auth.TenantContext, error) {
		p, err := engine.Catalog().GetPrincipal(actor)
		if err != nil {
			return auth.Principal{}, auth.TenantContext{}, err
		}
		return p, auth.TenantContext{TenantID: TenantID}, nil
	}

	viewReg := view.NewRegistry()
	if _, err := viewReg.Register(LabViewDefinition(), fp); err != nil {
		return nil, fmt.Errorf("register view: %w", err)
	}
	viewExec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    fp,
		Registry:  viewReg,
		Authorizer: &auth.ViewAuthorizer{
			Engine:   engine,
			TenantID: TenantID,
			Resolve:  resolve,
		},
		Audit: &view.AuditStoreAdapter{Store: auditStore, Now: now, NewID: newID},
		Now:   now,
	})
	if err != nil {
		return nil, fmt.Errorf("view executor: %w", err)
	}

	aiExec, err := ai.NewExecutor(ai.Config{
		Resources:             resources,
		Views:                 viewExec,
		Audit:                 &ai.AuditStoreAdapter{Store: auditStore, Now: now, NewID: newID},
		AuditRequired:         true,
		RequireConversationID: true,
		Policy: &auth.AIPolicyAdapter{
			Engine:   engine,
			TenantID: TenantID,
			Resolve:  resolve,
			Constraints: &auth.AIConstraints{
				Views: map[string]ai.ViewTypePolicy{
					ViewName: {MaxCount: 50},
				},
			},
		},
		ModelRouter: &ai.ModelRouter{Local: SeededStub{Seed: StubSeed}},
		Now:         now,
	})
	if err != nil {
		return nil, fmt.Errorf("ai executor: %w", err)
	}

	toolResult, err := aiExec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName:       ai.ToolRunView,
		Actor:          ActorID,
		TenantID:       TenantID,
		ConversationID: ConversationID,
		Input: map[string]any{
			"viewName": ViewName,
			"version":  ViewVersion,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("run_view: %w", err)
	}

	_, denied := aiExec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName:       ai.ToolRunView,
		Actor:          DeniedActorID,
		TenantID:       TenantID,
		ConversationID: ConversationID,
		Input: map[string]any{
			"viewName": ViewName,
			"version":  ViewVersion,
		},
	})
	if denied == nil {
		return nil, fmt.Errorf("research-ai-pipeline: denied actor %s was allowed run_view", DeniedActorID)
	}
	deniedErr := denied.Error()

	model, err := aiExec.InvokeModel(ctx, ai.ToolRequest{
		Actor:          ActorID,
		ConversationID: ConversationID,
	}, Prompt, toolResult.Context)
	if err != nil {
		return nil, fmt.Errorf("invoke stub: %w", err)
	}
	modelProv := ModelProvenance{Adapter: "seeded-stub", Seed: StubSeed}
	output := toolResult.Context
	if model != nil {
		modelProv.Adapter = model.Adapter
		modelProv.Content = model.Content
		output = model.Content
	}

	events, err := auditLogger.ListEvents(ctx, audit.Query{Limit: 100})
	if err != nil {
		return nil, err
	}

	rowCount := viewRowCount(toolResult.Data)

	bundle := ProvenanceBundle{
		Pipeline:    "research/ai-pipeline",
		Version:     PipelineVersion,
		GeneratedAt: FixedNow(),
		FAIR: FAIR{
			License:     "Apache-2.0",
			Synthetic:   true,
			ContainsPHI: false,
			Citation:    "See CITATION.cff",
		},
		Inputs: inputs,
		View: ViewProvenance{
			Name:     ViewName,
			Version:  ViewVersion,
			RowCount: rowCount,
		},
		Policy: PolicyProvenance{
			Version: "1",
			Hash:    policyHash(PolicyJSON()),
		},
		Tool: ToolProvenance{
			Name:    ai.ToolRunView,
			Outcome: toolResult.AuditMeta.Outcome,
		},
		Model:     modelProv,
		Citations: toolResult.Citations,
		Output:    output,
		Audit:     compactAudit(events),
	}

	return &Result{Bundle: bundle, ViewRows: rowCount, DeniedErr: deniedErr}, nil
}

func viewRowCount(data any) int {
	m, ok := data.(map[string]any)
	if !ok {
		return 0
	}
	switch rows := m["rows"].(type) {
	case []map[string]any:
		return len(rows)
	case []any:
		return len(rows)
	}
	switch total := m["total"].(type) {
	case int:
		return total
	case float64:
		return int(total)
	default:
		return 0
	}
}

func newAuthEngine() (*auth.Engine, error) {
	return auth.NewEngine(auth.Config{
		Roles: []auth.Role{{
			Name: "clinician",
			Permissions: []auth.Permission{
				"read-lab-summary",
				"observation.read",
			},
		}},
		Principals: []auth.Principal{
			{
				ID:   ActorID,
				Kind: auth.KindUser,
				TenantBindings: []auth.TenantBinding{{
					TenantID: TenantID,
					Roles:    []string{"clinician"},
				}},
			},
			{
				ID:   DeniedActorID,
				Kind: auth.KindUser,
				TenantBindings: []auth.TenantBinding{{
					TenantID: TenantID,
					Roles:    []string{},
				}},
			},
		},
		PolicyBytes:  PolicyJSON(),
		PolicyFormat: auth.PolicyFormatJSON,
	})
}

func sequentialID(prefix string) func() string {
	n := 0
	return func() string {
		n++
		return fmt.Sprintf("%s-%02d", prefix, n)
	}
}
