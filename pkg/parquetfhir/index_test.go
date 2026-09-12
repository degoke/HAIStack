package parquetfhir

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/validate"
)

func TestMergeResourceProfilesAddsChoiceTypes(t *testing.T) {
	base := bundledSD(t, "Observation")
	idx, err := newElementIndex(base)
	if err != nil {
		t.Fatalf("newElementIndex: %v", err)
	}
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{mustRawSD(t, "Observation")})
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromJSON: %v", err)
	}
	mergeResourceProfiles(idx, catalog, map[string]any{
		"resourceType": "Observation",
		"meta": map[string]any{
			"profile": []any{"http://hl7.org/fhir/StructureDefinition/Observation"},
		},
	})
	if typ := idx.resolveFieldType("Observation.valueQuantity.value", "value", "Quantity"); typ != "decimal" {
		t.Fatalf("Quantity.value type=%q, want decimal", typ)
	}
}

func mustRawSD(t *testing.T, resourceType string) []byte {
	t.Helper()
	return bundledSDRaw(t, resourceType)
}
