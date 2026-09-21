// Command ai-pipeline runs the reproducible FHIR → view → AI provenance demo.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/degoke/health-ai-stack/pkg/ai"
	"github.com/degoke/health-ai-stack/pkg/audit"
	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/degoke/health-ai-stack/pkg/view"
	"github.com/degoke/health-ai-stack/research/internal/researchutil"
)

//go:embed views/research_vitals_view.json
var vitalsView []byte

const (
	viewName    = "research_vitals_view"
	viewVersion = "1.0.0"
	modelSeed   = int64(11)
)

var pipelinePolicy = []byte(`{
  "version": "1",
  "rules": [
    {
      "name": "patient-and-observation-read",
      "effect": "allow",
      "match": {
        "actions": ["read"],
        "resourceTypes": ["Patient", "Observation"],
        "anyPermissions": ["patient.read", "observation.read"]
      },
      "reason": "clinician may read patients and observations"
    },
    {
      "name": "vitals-view",
      "effect": "allow",
      "match": {
        "actions": ["execute-view"],
        "viewNames": ["research_vitals_view"],
        "anyPermissions": ["observation.read"]
      },
      "reason": "clinician may execute the research vitals view"
    },
    {
      "name": "ai-run-view",
      "effect": "allow",
      "match": {
        "actions": ["execute-ai-tool"],
        "toolNames": ["run_view"]
      },
      "reason": "clinician may invoke run_view"
    }
  ]
}`)

func main() {
	if err := printPipeline(); err != nil {
		fmt.Fprintf(os.Stderr, "ai-pipeline: %v\n", err)
		os.Exit(1)
	}
}

func printPipeline() error {
	bundle, err := Run(context.Background())
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(bundle)
}

// Run executes the reproducible FHIR → view → AI tool → audit pipeline.
func Run(ctx context.Context) (*ProvenanceBundle, error) {
	now := researchutil.FixedTime
	resources := researchutil.NewMemoryResourceStore()
	auditStore := audit.NewMemoryStore()

	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		return nil, err
	}
	inputs, validation, err := loadAndValidate(ctx, resources, engine)
	if err != nil {
		return nil, err
	}
	authEng, err := auth.NewEngine(auth.Config{
		Roles: []auth.Role{{
			Name: "clinician",
			Permissions: []auth.Permission{
				"patient.read",
				"observation.read",
			},
		}},
		Principals: []auth.Principal{{
			ID:   pipelineActor,
			Kind: auth.KindUser,
			TenantBindings: []auth.TenantBinding{{
				TenantID: pipelineTenant,
				Roles:    []string{"clinician"},
			}},
		}},
		PolicyBytes:  pipelinePolicy,
		PolicyFormat: auth.PolicyFormatJSON,
	})
	if err != nil {
		return nil, err
	}

	resolve := func(_ context.Context, actor, _ string) (auth.Principal, auth.TenantContext, error) {
		p, err := authEng.Catalog().GetPrincipal(actor)
		if err != nil {
			return auth.Principal{}, auth.TenantContext{}, err
		}
		return p, auth.TenantContext{TenantID: pipelineTenant}, nil
	}

	viewReg := view.NewRegistry()
	if _, err := viewReg.Register(vitalsView, engine); err != nil {
		return nil, fmt.Errorf("register view: %w", err)
	}
	viewExec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    engine,
		Registry:  viewReg,
		Authorizer: &auth.ViewAuthorizer{
			Engine:   authEng,
			TenantID: pipelineTenant,
			Resolve:  resolve,
		},
		Audit: &view.AuditStoreAdapter{Store: auditStore, Now: now},
		Now:   now,
	})
	if err != nil {
		return nil, err
	}

	stub := &StubModel{Adapter: "stub-v1", Seed: modelSeed}
	aiExec, err := ai.NewExecutor(ai.Config{
		Resources:             resources,
		Views:                 viewExec,
		Audit:                 &ai.AuditStoreAdapter{Store: auditStore, Now: now},
		AuditRequired:         true,
		RequireConversationID: true,
		Now:                   now,
		ModelRouter:           &ai.ModelRouter{Local: stub},
		Policy: &auth.AIPolicyAdapter{
			Engine:   authEng,
			TenantID: pipelineTenant,
			Resolve:  resolve,
			Constraints: &auth.AIConstraints{
				Views: map[string]ai.ViewTypePolicy{
					viewName: {},
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	toolRes, err := aiExec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName:       ai.ToolRunView,
		Actor:          pipelineActor,
		TenantID:       pipelineTenant,
		Subject:        "research/vitals",
		ConversationID: conversationID,
		Input: map[string]any{
			"viewName": viewName,
			"version":  viewVersion,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("run_view: %w", err)
	}

	modelRes, err := aiExec.InvokeModel(ctx, ai.ToolRequest{
		Actor:          pipelineActor,
		TenantID:       pipelineTenant,
		ConversationID: conversationID,
		ModelHint:      "local",
	}, "Summarize permissioned vitals for research evaluation.", toolRes.Context)
	if err != nil {
		return nil, fmt.Errorf("invoke model: %w", err)
	}
	if modelRes == nil {
		return nil, fmt.Errorf("stub model returned nil")
	}

	rows, columns := viewRows(toolRes.Data)
	rowHash, err := researchutil.HashJSON(rows)
	if err != nil {
		return nil, err
	}
	ctxHash := researchutil.HashBytes([]byte(toolRes.Context))
	policyHash := researchutil.HashBytes(pipelinePolicy)
	defHash := researchutil.HashBytes(vitalsView)

	if len(rows) == 0 {
		return nil, fmt.Errorf("expected permissioned view rows, got 0")
	}
	// preliminary observation is filtered out of the view
	if len(rows) != 5 {
		return nil, fmt.Errorf("expected 5 final vitals rows, got %d", len(rows))
	}

	events, err := auditStore.List(ctx, store.AuditQuery{})
	if err != nil {
		return nil, err
	}
	if !hasAuditAction(events, audit.ActionExecuteView) {
		return nil, fmt.Errorf("missing %s audit event", audit.ActionExecuteView)
	}
	if !hasAuditAction(events, audit.ActionExecuteTool) {
		return nil, fmt.Errorf("missing %s audit event", audit.ActionExecuteTool)
	}
	if !hasAuditAction(events, audit.ActionInvokeModel) {
		return nil, fmt.Errorf("missing %s audit event", audit.ActionInvokeModel)
	}

	return &ProvenanceBundle{
		Artefact:  "haistack-research-ai-pipeline",
		Track:     "E",
		CreatedAt: now(),
		FAIR: FAIRMetadata{
			License:   "Apache-2.0",
			Synthetic: true,
			PHI:       false,
			Citation:  "See CITATION.cff and research/README.md",
		},
		Inputs:     inputs,
		Validation: validation,
		View: ViewProvenance{
			Name:       viewName,
			Version:    viewVersion,
			Definition: defHash,
			RowCount:   len(rows),
			RowHash:    rowHash,
			Columns:    columns,
		},
		Policy: PolicyProvenance{
			Version: "1",
			Hash:    policyHash,
			Format:  "json",
		},
		Tool: ToolProvenance{
			Name:      ai.ToolRunView,
			Actor:     pipelineActor,
			Outcome:   toolRes.AuditMeta.Outcome,
			Citations: toolRes.Citations,
		},
		Model: ModelProvenance{
			Adapter: stub.Name(),
			Seed:    modelSeed,
		},
		Output: OutputProvenance{
			Content: modelRes.Content,
			Context: ctxHash,
		},
		Audit: events,
	}, nil
}

func loadAndValidate(ctx context.Context, resources store.ResourceStore, engine fhirpath.Engine) ([]InputRecord, ValidationProvenance, error) {
	catalog, pin, err := loadPinnedCatalog()
	if err != nil {
		return nil, ValidationProvenance{}, err
	}
	validator, err := validate.NewEngine(validate.Config{
		ProfileCatalog: catalog,
		FHIRPath:       engine,
	})
	if err != nil {
		return nil, pin, err
	}
	var inputs []InputRecord
	var envelopes []*types.ResourceEnvelope
	for _, p := range pipelinePatients() {
		env, err := patientEnvelope(p)
		if err != nil {
			return nil, pin, err
		}
		envelopes = append(envelopes, env)
	}
	for _, o := range pipelineObservations() {
		env, err := observationEnvelope(o)
		if err != nil {
			return nil, pin, err
		}
		envelopes = append(envelopes, env)
	}
	opts := validate.ValidateOptions{
		ProfileCatalog:          catalog,
		EnforceBaseProfile:      true,
		EnforceDeclaredProfiles: true,
		Mode:                    validate.ValidationModeFast,
	}
	for _, env := range envelopes {
		result, err := validator.Validate(ctx, env, opts)
		if err != nil {
			return nil, pin, fmt.Errorf("validate %s/%s: %w", env.ResourceType, env.ID, err)
		}
		if result == nil || !result.Valid {
			return nil, pin, fmt.Errorf("validate %s/%s: invalid %+v", env.ResourceType, env.ID, result)
		}
		if err := resources.Create(ctx, env); err != nil {
			return nil, pin, err
		}
		profile := validate.BaseStructureDefinitionURL(env.ResourceType)
		if env.ResourceType == "Patient" {
			profile = haiPatientProfileURL
		}
		inputs = append(inputs, InputRecord{
			ResourceType: env.ResourceType,
			ID:           env.ID,
			Hash:         env.Hash,
			Validated:    true,
			Profile:      profile,
		})
	}
	return inputs, pin, nil
}

type conformanceLock struct {
	IGPackage struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Canonical string `json:"canonical"`
	} `json:"igPackage"`
	FHIRVersion string `json:"fhirVersion"`
	GitCommit   string `json:"gitCommit"`
}

func loadPinnedCatalog() (validate.MemoryProfileCatalog, ValidationProvenance, error) {
	root, err := researchutil.RepoRoot()
	if err != nil {
		return nil, ValidationProvenance{}, err
	}
	lockPath := filepath.Join(root, "conformance-lock.json")
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		return nil, ValidationProvenance{}, fmt.Errorf("read conformance-lock: %w", err)
	}
	var lock conformanceLock
	if err := json.Unmarshal(lockBytes, &lock); err != nil {
		return nil, ValidationProvenance{}, err
	}
	igRel := "modules/core/ig"
	pin := ValidationProvenance{
		FHIRVersion:           lock.FHIRVersion,
		IGPackage:             lock.IGPackage.Name,
		IGVersion:             lock.IGPackage.Version,
		Canonical:             lock.IGPackage.Canonical,
		ConformanceLockCommit: lock.GitCommit,
		CheckoutCommit:        researchutil.CheckoutCommit(root),
		Mode:                  "r4-base-and-declared-ig",
		Profiles: []string{
			validate.BaseStructureDefinitionURL("Patient"),
			validate.BaseStructureDefinitionURL("Observation"),
			haiPatientProfileURL,
		},
		IGResources: igRel,
	}
	sdDir := filepath.Join(root, "pkg/registry/internal/bundles/r4/structure-definitions")
	var resources [][]byte
	for _, name := range []string{"Patient.json", "Observation.json"} {
		raw, err := os.ReadFile(filepath.Join(sdDir, name))
		if err != nil {
			return nil, pin, fmt.Errorf("load %s: %w", name, err)
		}
		resources = append(resources, raw)
	}
	catalog, err := validate.LoadProfileCatalogFromJSON(resources)
	if err != nil {
		return nil, pin, err
	}
	igDir := filepath.Join(root, igRel)
	ig, err := validate.LoadProfileCatalogFromDir(igDir)
	if err != nil {
		return nil, pin, fmt.Errorf("load compiled IG from %s: %w", igRel, err)
	}
	if _, ok := ig.GetStructureDefinition(haiPatientProfileURL); !ok {
		return nil, pin, fmt.Errorf("compiled IG %s is missing %s", igRel, haiPatientProfileURL)
	}
	return validate.MergeProfileCatalogs(catalog, ig), pin, nil
}

func viewRows(data any) ([]map[string]any, []string) {
	switch v := data.(type) {
	case *view.Result:
		cols := make([]string, 0, len(v.Columns))
		for _, c := range v.Columns {
			cols = append(cols, c.Name)
		}
		return v.Rows, cols
	case map[string]any:
		rows, _ := v["rows"].([]map[string]any)
		var cols []string
		switch c := v["columns"].(type) {
		case []view.ColumnInfo:
			for _, col := range c {
				cols = append(cols, col.Name)
			}
		case []string:
			cols = append(cols, c...)
		}
		return rows, cols
	default:
		return nil, nil
	}
}

func hasAuditAction(events []store.AuditRecord, action string) bool {
	for _, e := range events {
		if e.Action == action {
			return true
		}
	}
	return false
}
