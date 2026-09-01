package conformance_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/conformance"
)

func TestValidateIGExamples(t *testing.T) {
	root := repoRoot(t)
	igDir := filepath.Join(root, "conformance/fsh-generated/resources")
	if _, err := os.Stat(igDir); err != nil {
		t.Skip("IG resources missing; run make ig")
	}
	cfg := conformance.IGValidatorConfig{
		BaseSDDir:      filepath.Join(root, "pkg/registry/internal/bundles/r4/structure-definitions"),
		IGResourcesDir: filepath.Join(root, "conformance/fsh-generated/resources"),
		ValidDir:       filepath.Join(root, "conformance/examples/valid"),
		InvalidDir:     filepath.Join(root, "conformance/examples/invalid"),
	}
	if err := conformance.ValidateIG(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}
