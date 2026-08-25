package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/degoke/health-ai-stack/examples/internal/appkit"
	"github.com/degoke/health-ai-stack/pkg/ai"
	"github.com/degoke/health-ai-stack/pkg/auth"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ai-authz: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "haistack-ai-authz-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	stack, err := appkit.NewSQLiteStack(ctx, filepath.Join(tempDir, "ai.db"), "Patient")
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	patient, err := appkit.EnvelopeFromJSON("Patient", appkit.PatientJSON("Mary", "Jackson", "+1-555-0130"))
	if err != nil {
		return err
	}
	created, err := stack.ResourceService.Create(ctx, patient)
	if err != nil {
		return err
	}

	authEngine, err := auth.NewEngine(auth.Config{
		Roles: []auth.Role{{
			Name:        "clinician",
			Permissions: []auth.Permission{"patient.read"},
		}},
		Principals: []auth.Principal{{
			ID:   "user-1",
			Kind: auth.KindUser,
			TenantBindings: []auth.TenantBinding{{
				TenantID: "tenant-demo",
				Roles:    []string{"clinician"},
			}},
		}},
		PolicyBytes: []byte(`{
  "version": "1",
  "rules": [
    {
      "name": "allow-patient-read",
      "effect": "allow",
      "match": {
        "actions": ["read"],
        "resourceTypes": ["Patient"],
        "anyPermissions": ["patient.read"]
      },
      "reason": "clinicians may read patients"
    }
  ]
}`),
		PolicyFormat: auth.PolicyFormatJSON,
	})
	if err != nil {
		return err
	}

	resolve := func(_ context.Context, actor, _ string) (auth.Principal, auth.TenantContext, error) {
		principal, err := authEngine.Catalog().GetPrincipal(actor)
		if err != nil {
			return auth.Principal{}, auth.TenantContext{}, err
		}
		return principal, auth.TenantContext{TenantID: "tenant-demo"}, nil
	}

	aiExec, err := ai.NewExecutor(ai.Config{
		Resources:     stack.DB.ResourceStore(),
		Search:        stack.SearchService,
		Core:          stack.ResourceService,
		Audit:         &ai.AuditStoreAdapter{Store: stack.DB.AuditStore()},
		AuditRequired: true,
		Policy: &auth.AIPolicyAdapter{
			Engine:              authEngine,
			TenantID:            "tenant-demo",
			Resolve:             resolve,
			PatientSearchParams: stack.Snapshot,
			Constraints: &auth.AIConstraints{
				Read: map[string]ai.ReadTypePolicy{
					"Patient": {AllowedFields: []string{"name", "telecom"}},
				},
				Search: map[string]ai.SearchTypePolicy{
					"Patient": {AllowedParams: []string{"name"}, AllowedFields: []string{"name"}, MaxCount: 5},
				},
			},
		},
	})
	if err != nil {
		return err
	}

	readResult, err := aiExec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolReadFhirResource,
		Actor:    "user-1",
		Subject:  "patient/" + created.ID,
		Input: map[string]any{
			"resourceType": "Patient",
			"id":           created.ID,
		},
	})
	if err != nil {
		return err
	}

	searchResult, err := aiExec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolSearchFhirResources,
		Actor:    "user-1",
		Subject:  "patient/" + created.ID,
		Input: map[string]any{
			"resourceType": "Patient",
			"params": map[string]any{
				"name": "Mary",
			},
		},
	})
	if err != nil {
		return err
	}

	_, deniedErr := aiExec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolSearchFhirResources,
		Actor:    "user-1",
		Subject:  "patient/" + created.ID,
		Input: map[string]any{
			"resourceType": "Patient",
			"params": map[string]any{
				"telecom": "+1-555-0130",
			},
		},
	})

	fmt.Println("AI tools with auth-backed policy")
	fmt.Println("read_fhir_resource context:")
	fmt.Println(readResult.Context)
	fmt.Println("search_fhir_resources context:")
	fmt.Println(searchResult.Context)
	if deniedErr != nil {
		fmt.Printf("expected denied search error: %v\n", deniedErr)
	}
	if deniedErr == nil || !errors.Is(deniedErr, ai.ErrPolicyDenied) {
		return fmt.Errorf("expected a policy denial for telecom search, got %v", deniedErr)
	}
	return nil
}
