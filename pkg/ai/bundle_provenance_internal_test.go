package ai

import "testing"

func TestProvenanceTargetsExplicitInSpecs(t *testing.T) {
	specs := []transactionEntrySpec{
		{Method: "POST", ResourceType: "Patient", Fields: map[string]any{"gender": "female"}},
		{
			Method: "POST", ResourceType: "Provenance",
			Fields: map[string]any{
				"target": []any{map[string]any{"reference": "Patient/p1"}},
			},
		},
	}
	explicit := provenanceTargetsExplicitInSpecs(specs)
	if !explicit["Patient/p1"] {
		t.Fatalf("expected Patient/p1 in explicit targets: %#v", explicit)
	}
	if provenanceTargetCovered(explicit, "Patient/p1") != true {
		t.Fatal("expected covered")
	}
	if provenanceTargetCovered(explicit, "Patient/p2") {
		t.Fatal("unexpected cover")
	}
}
