package ai

import "testing"

func TestParseBundleInput_BatchType(t *testing.T) {
	typ, specs, err := parseBundleInput(map[string]any{
		"bundleType": "batch",
		"entries": []any{
			map[string]any{
				"method": "GET", "resourceType": "Patient", "id": "p1",
			},
		},
	})
	if err != nil || typ != "batch" || len(specs) != 1 {
		t.Fatalf("typ=%q specs=%d err=%v", typ, len(specs), err)
	}
}
