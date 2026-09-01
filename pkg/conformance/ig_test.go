package conformance_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/conformance"
)

func TestValidateIGExamples(t *testing.T) {
	root, err := conformance.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	igDir := filepath.Join(root, "conformance/fsh-generated/resources")
	if _, err := os.Stat(igDir); err != nil {
		t.Skip("IG resources missing; run make ig")
	}
	if err := conformance.ValidateIG(context.Background(), conformance.DefaultIGValidatorConfig(root)); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidExampleSidecarsRequireMustFail(t *testing.T) {
	root, err := conformance.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	invalidDir := filepath.Join(root, "conformance/examples/invalid")
	entries, err := os.ReadDir(invalidDir)
	if err != nil {
		t.Skip("invalid examples missing")
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !stringsHasSuffix(name, ".expected.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(invalidDir, name))
		if err != nil {
			t.Fatal(err)
		}
		var expected struct {
			MustFail bool `json:"mustFail"`
		}
		if err := json.Unmarshal(data, &expected); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		if !expected.MustFail {
			t.Fatalf("%s must set mustFail: true", name)
		}
	}
}

func stringsHasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
