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
	if _, ok := file.Principals["clinician"]; !ok {
		t.Fatal("testdata catalogue missing clinician principal")
	}
	kit := NewDefaultKit(DefaultEngine(t))
	Run(t, scenarios, kit)
}

func TestParseYAMLRequiresPrincipals(t *testing.T) {
	_, err := ParseYAML([]byte(`
version: "1"
roles:
  - name: clinician
    permissions: [patient.read]
policies:
  base:
    version: "1"
    rules:
      - name: allow
        effect: allow
        match:
          actions: [read]
scenarios:
  - name: missing_principal_catalogue
    action: read
    resourceType: Patient
    expectAllow: true
`))
	if err == nil {
		t.Fatal("expected error for catalogue without principals")
	}
}

func TestScenariosFromYAMLUnknownPrincipal(t *testing.T) {
	file, err := ParseYAML([]byte(`
version: "1"
roles:
  - name: clinician
    permissions: [patient.read]
principals:
  clinician:
    id: user-clinician
    kind: user
    tenant: tenant-a
    roles: [clinician]
policies:
  base:
    version: "1"
    rules:
      - name: allow
        effect: allow
        match:
          actions: [read]
scenarios:
  - name: unknown_actor
    principal: ghost
    action: read
    resourceType: Patient
    expectAllow: true
`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = ScenariosFromYAML(file)
	if err == nil {
		t.Fatal("expected unknown principal error")
	}
}
