package core

import "testing"

func TestApplyPathUpdates_SimpleElement(t *testing.T) {
	root := map[string]any{
		"resourceType": "Patient",
		"gender":       "unknown",
	}
	if err := ApplyPathUpdates("Patient", root, map[string]any{"gender": "female"}); err != nil {
		t.Fatal(err)
	}
	if root["gender"] != "female" {
		t.Fatalf("gender = %v", root["gender"])
	}
}

func TestApplyPathUpdates_NestedPath(t *testing.T) {
	root := map[string]any{
		"resourceType": "Patient",
		"name":         []any{map[string]any{"family": "Old"}},
	}
	if err := ApplyPathUpdates("Patient", root, map[string]any{"name[0].family": "New"}); err != nil {
		t.Fatal(err)
	}
	names := root["name"].([]any)
	fam := names[0].(map[string]any)["family"]
	if fam != "New" {
		t.Fatalf("family = %v", fam)
	}
}
