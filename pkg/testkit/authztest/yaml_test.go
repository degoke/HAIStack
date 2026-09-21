package authztest

import (
	_ "embed"
	"testing"
)

//go:embed testdata/intersection.yaml
var intersectionYAML []byte

func TestYAMLIntersectionCatalogue(t *testing.T) {
	file, err := ParseYAML(intersectionYAML)
	if err != nil {
		t.Fatal(err)
	}
	scenarios, err := ScenariosFromYAML(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCatalog(scenarios); err != nil {
		t.Fatal(err)
	}
	if len(scenarios) < 2 {
		t.Fatalf("testdata catalogue too small: %d", len(scenarios))
	}
	kit := NewDefaultKit(DefaultEngine(t))
	Run(t, scenarios, kit)
}
