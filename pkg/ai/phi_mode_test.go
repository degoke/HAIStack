package ai_test

import (
	"context"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
	"github.com/degoke/haistack/pkg/validate"
)

func TestMergedBundleCachePerProfiles(t *testing.T) {
	snapshot := testSnapshot(t, "Patient")
	profiles := validate.NewRegistryProfileCatalog(snapshot)
	deid := ai.NewFHIRDeidentifierWithConfig(ai.FHIRDeidentifierConfig{
		Catalog:  ai.DefaultPHICatalog(),
		Profiles: profiles,
		Mode:     ai.PHIModeStandard,
	})
	ctx := context.Background()
	data := map[string]any{
		"resourceType": "Patient",
		"id":           "p1",
		"meta": map[string]any{
			"profile": []any{"http://hl7.org/fhir/StructureDefinition/Patient"},
		},
	}
	_, _, err := deid.Deidentify(ctx, ai.DeidentifyRequest{
		ToolName: ai.ToolReadFhirResource, ResourceType: "Patient", Data: data,
	})
	if err != nil {
		t.Fatalf("Deidentify: %v", err)
	}
}

func TestSearchSkipsFHIRPathEval(t *testing.T) {
	deid := ai.NewFHIRDeidentifierWithConfig(ai.FHIRDeidentifierConfig{
		Catalog:  ai.DefaultPHICatalog(),
		EvalMode: ai.EvalModeAlways,
	})
	data := map[string]any{
		"resources": []any{
			map[string]any{
				"resourceType": "Patient",
				"id":           "p1",
				"name":         []any{map[string]any{"family": "Doe"}},
			},
		},
	}
	_, _, err := deid.Deidentify(context.Background(), ai.DeidentifyRequest{
		ToolName: ai.ToolSearchFhirResources,
		Data:     data,
	})
	if err != nil {
		t.Fatalf("Deidentify: %v", err)
	}
}
