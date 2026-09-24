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
	deid, err := ai.NewFHIRDeidentifierWithConfig(ai.FHIRDeidentifierConfig{
		Catalog:  ai.DefaultPHICatalog(),
		Profiles: profiles,
		Mode:     ai.PHIModeStandard,
	})
	if err != nil {
		t.Fatalf("NewFHIRDeidentifierWithConfig: %v", err)
	}
	ctx := context.Background()
	data := map[string]any{
		"resourceType": "Patient",
		"id":           "p1",
		"meta": map[string]any{
			"profile": []any{"http://hl7.org/fhir/StructureDefinition/Patient"},
		},
	}
	_, _, deidErr := deid.Deidentify(ctx, ai.DeidentifyRequest{
		ToolName: ai.ToolReadFhirResource, ResourceType: "Patient", Data: data,
	})
	if deidErr != nil {
		t.Fatalf("Deidentify: %v", deidErr)
	}
}

func TestSearchSkipsFHIRPathEval(t *testing.T) {
	deid, err := ai.NewFHIRDeidentifierWithConfig(ai.FHIRDeidentifierConfig{
		Catalog:  ai.DefaultPHICatalog(),
		EvalMode: ai.EvalModeAlways,
	})
	if err != nil {
		t.Fatalf("NewFHIRDeidentifierWithConfig: %v", err)
	}
	data := map[string]any{
		"resources": []any{
			map[string]any{
				"resourceType": "Patient",
				"id":           "p1",
				"name":         []any{map[string]any{"family": "Doe"}},
			},
		},
	}
	_, _, searchErr := deid.Deidentify(context.Background(), ai.DeidentifyRequest{
		ToolName: ai.ToolSearchFhirResources,
		Data:     data,
	})
	if searchErr != nil {
		t.Fatalf("Deidentify: %v", searchErr)
	}
}
