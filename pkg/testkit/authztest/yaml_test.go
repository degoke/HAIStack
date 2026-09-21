package authztest

import (
	"context"
	_ "embed"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/auth"
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
	RunYAML(t, scenarios)
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

func TestParseYAMLRequiresPrincipalTenant(t *testing.T) {
	_, err := ParseYAML([]byte(`
version: "1"
roles:
  - name: clinician
    permissions: [patient.read]
principals:
  clinician:
    id: user-clinician
    kind: user
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
  - name: missing_tenant
    principal: clinician
    action: read
    resourceType: Patient
    expectAllow: true
`))
	if err == nil {
		t.Fatal("expected error for principal without tenant")
	}
	if !strings.Contains(err.Error(), `principal "clinician" missing tenant`) {
		t.Fatalf("err = %v, want missing tenant", err)
	}
}

func TestParseYAMLRejectsWhitespaceTenant(t *testing.T) {
	_, err := ParseYAML([]byte(`
version: "1"
roles:
  - name: clinician
    permissions: [patient.read]
principals:
  clinician:
    id: user-clinician
    kind: user
    tenant: "   "
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
  - name: blank_tenant
    principal: clinician
    action: read
    resourceType: Patient
    expectAllow: true
`))
	if err == nil {
		t.Fatal("expected error for whitespace-only tenant")
	}
	if !strings.Contains(err.Error(), "missing tenant") {
		t.Fatalf("err = %v, want missing tenant", err)
	}
}

func TestAdapterForYAMLPrincipalRequiresTenant(t *testing.T) {
	_, err := adapterForYAMLPrincipal(YAMLPrincipal{
		ID:    "user-clinician",
		Kind:  "user",
		Roles: []string{"clinician"},
	})
	if err == nil {
		t.Fatal("expected error for principal without tenant")
	}
	if !strings.Contains(err.Error(), "missing tenant") {
		t.Fatalf("err = %v, want missing tenant", err)
	}
}

func TestYAMLPrincipalDrivesSMARTBundle(t *testing.T) {
	adapter, bundle, err := bundleForYAML(YAMLScenario{
		Principal: "admin",
		Scopes:    "user/*.read",
	}, map[string]YAMLPrincipal{
		"admin": {
			ID:     "user-admin",
			Kind:   "user",
			Tenant: "tenant-b",
			Roles:  []string{"tenant-admin"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if adapter == nil {
		t.Fatal("expected adapter from YAML principal")
	}
	if bundle.Principal.ID != "user-admin" {
		t.Fatalf("id = %q", bundle.Principal.ID)
	}
	if bundle.Tenant.TenantID != "tenant-b" {
		t.Fatalf("tenant = %q, want tenant-b from YAML principal", bundle.Tenant.TenantID)
	}
	if len(bundle.Tenant.RoleBindings) != 1 || bundle.Tenant.RoleBindings[0] != "tenant-admin" {
		t.Fatalf("role bindings = %#v, want tenant-admin from YAML principal", bundle.Tenant.RoleBindings)
	}
	if len(bundle.Principal.TenantBindings) == 0 || len(bundle.Principal.TenantBindings[0].Roles) == 0 || bundle.Principal.TenantBindings[0].Roles[0] != "tenant-admin" {
		t.Fatalf("principal bindings = %#v", bundle.Principal.TenantBindings)
	}
}

func TestEngineForYAMLPolicyOmitsGoDevices(t *testing.T) {
	eng, err := engineForYAMLPolicy(auth.PolicyDocument{
		Version: "1",
		Rules: []auth.PolicyRule{{
			Name:   "allow-read",
			Effect: auth.EffectAllow,
			Match:  auth.RuleMatch{Actions: []string{auth.ActionRead}},
		}},
	}, []auth.Role{{
		Name:        "clinician",
		Permissions: []auth.Permission{"patient.read"},
	}}, []auth.Principal{{
		ID:   "user-clinician",
		Kind: auth.KindUser,
		TenantBindings: []auth.TenantBinding{{
			TenantID: TenantA,
			Roles:    []string{"clinician"},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Catalog().GetDevice("device-trusted"); err == nil {
		t.Fatal("YAML engine should not include Go BaseConfig devices")
	}
}

func TestYAMLUnmatchedDenyIsPolicyNotMissingPermission(t *testing.T) {
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
      - name: patient-read
        effect: allow
        match:
          actions: [read]
          resourceTypes: [Patient]
          anyPermissions: [patient.read]
scenarios:
  - name: unmatched_type
    principal: clinician
    scopes: user/*.read
    policy: base
    roleGrants:
      clinician: ["*.read"]
    action: read
    resourceType: MedicationRequest
    resourceId: rx-1
    expectAllow: false
`))
	if err != nil {
		t.Fatal(err)
	}
	spec := file.Scenarios[0]
	doc, err := resolveYAMLPolicy(file, spec)
	if err != nil {
		t.Fatal(err)
	}
	roles := resolveYAMLRoles(file, spec)
	principals, err := principalsFromYAML(file)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engineForYAMLPolicy(doc, roles, principals)
	if err != nil {
		t.Fatal(err)
	}
	adapter, bundle, err := bundleForYAML(spec, file.Principals)
	if err != nil {
		t.Fatal(err)
	}
	_, decision, err := evaluateYAMLIntersection(context.Background(), eng, adapter, bundle, spec)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed {
		t.Fatal("expected deny")
	}
	if strings.Contains(decision.Reason, "missing required permissions") {
		t.Fatalf("denied at RequiredPermissions, want unmatched policy: %s", decision.Reason)
	}
}
