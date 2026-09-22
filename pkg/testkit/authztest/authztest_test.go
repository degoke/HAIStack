package authztest_test

import (
	"context"
	"testing"

	"github.com/degoke/haistack/pkg/testkit/authztest"
)

func TestAuthorizationScenarioCatalog(t *testing.T) {
	scenarios := authztest.AllScenarios()
	if err := authztest.ValidateCatalog(scenarios); err != nil {
		t.Fatal(err)
	}
	if len(scenarios) < 30 {
		t.Fatalf("expected at least 30 scenarios, got %d", len(scenarios))
	}
	t.Logf("running %d documented authorization scenarios", len(scenarios))

	eng := authztest.DefaultEngine(t)
	kit := authztest.NewDefaultKit(eng)
	authztest.RunAll(t, kit)
}

func TestCatalogSize(t *testing.T) {
	if authztest.CatalogSize() < 30 {
		t.Fatalf("catalog size = %d, want >= 30", authztest.CatalogSize())
	}
}

func TestDeclarativeFixtureCatalog(t *testing.T) {
	path, err := authztest.DeclarativeFixturePath()
	if err != nil {
		t.Skip(err)
	}
	scenarios, err := authztest.LoadDeclarativeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := authztest.RunDeclarative(context.Background(), scenarios); err != nil {
		t.Fatal(err)
	}
}
