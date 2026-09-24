package ai

import (
	"testing"
)

func TestStrictRedactionMapAndJSONParity(t *testing.T) {
	catalog := DefaultPHICatalog()
	fixtures := []map[string]any{
		{
			"resourceType": "Patient",
			"id":           "p1",
			"meta": map[string]any{
				"security": []any{
					map[string]any{
						"system": V3ConfidentialityCodeSystem,
						"code":   "R",
					},
				},
			},
		},
		{
			"resourceType": "Patient",
			"id":           "p2",
			"meta": map[string]any{
				"security": []any{
					map[string]any{
						"system": "http://terminology.hl7.org/CodeSystem/security-labels",
						"code":   "PSY",
					},
				},
			},
		},
		{
			"resourceType": "Patient",
			"id":           "p3",
			"gender":       "female",
		},
	}
	for _, m := range fixtures {
		fromMap := catalog.resourceRequiresStrictRedaction(m)
		raw, err := marshalJSONPooled(m)
		if err != nil {
			t.Fatal(err)
		}
		fromJSON := strictRedactionFromJSON(raw, catalog)
		if fromMap != fromJSON {
			t.Fatalf("strict mismatch for id=%v map=%v json=%v", m["id"], fromMap, fromJSON)
		}
	}
}
