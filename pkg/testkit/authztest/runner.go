package authztest

import (
	"context"
	"fmt"
	"testing"
)

// Run executes every scenario, failing the test on the first error.
func Run(t *testing.T, scenarios []Scenario, kit *Kit) {
	t.Helper()
	for _, sc := range scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			if sc.Doc != "" {
				t.Log("scenario:", sc.Doc)
			}
			if err := sc.Run(context.Background(), kit); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// RunAll executes the full documented scenario catalog.
func RunAll(t *testing.T, kit *Kit) {
	Run(t, AllScenarios(), kit)
}

// ValidateCatalog ensures every scenario has a name and run function.
func ValidateCatalog(scenarios []Scenario) error {
	seen := make(map[string]struct{}, len(scenarios))
	for _, sc := range scenarios {
		if sc.Name == "" {
			return fmt.Errorf("authztest: scenario missing name")
		}
		if sc.Run == nil {
			return fmt.Errorf("authztest: scenario %q missing Run", sc.Name)
		}
		if _, dup := seen[sc.Name]; dup {
			return fmt.Errorf("authztest: duplicate scenario name %q", sc.Name)
		}
		seen[sc.Name] = struct{}{}
	}
	return nil
}

// CatalogSize returns the number of documented scenarios.
func CatalogSize() int {
	return len(AllScenarios())
}
