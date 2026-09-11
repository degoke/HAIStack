package registry_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/registry"
)

func TestModulesPackageID(t *testing.T) {
	if registry.ModulesPackageID("core") != "haistack-modules/core" {
		t.Fatalf("got %q", registry.ModulesPackageID("core"))
	}
	if registry.ModulesPackageID("") != registry.ModulesPackageName {
		t.Fatalf("empty module name should return prefix only")
	}
}
