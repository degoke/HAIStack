package ai

import "testing"

func TestCodingHasAIAST_NestedCoding(t *testing.T) {
	security := []any{
		map[string]any{
			"coding": []any{
				map[string]any{
					"system": AIASTCodeSystem,
					"code":   AIASTCode,
				},
			},
		},
	}
	if !hasAIASTCoding(security) {
		t.Fatal("expected nested coding to match AIAST")
	}
}

func TestMergeAIASTMeta_UsesExtensionNotSource(t *testing.T) {
	root := map[string]any{
		"resourceType": "Patient",
		"meta": map[string]any{
			"source": "https://ehr.example/patient-feed",
		},
	}
	mergeAIASTMeta(root, "conv-9", "agent-1")
	meta := root["meta"].(map[string]any)
	if meta["source"] != "https://ehr.example/patient-feed" {
		t.Fatalf("meta.source = %v", meta["source"])
	}
	exts, ok := meta["extension"].([]any)
	if !ok || len(exts) == 0 {
		t.Fatal("expected ai context extension")
	}
}
