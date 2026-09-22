package authztest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

// DeclarativeScenario is a portable principal+policy+request case for tests.
// The canonical Track C artefact is research/policy-semantics/scenarios.yaml.
// JSON fixtures (testdata/declarative-scenarios.json) exercise compartment
// overlay via resource bodies and legacy BaseConfig policies in tests only.
// Consent overlays are skipped by RunDeclarative.
type DeclarativeScenario struct {
	ID            string          `json:"id"`
	Doc           string          `json:"doc"`
	Principal     string          `json:"principal"`
	Policy        string          `json:"policy"`
	Scopes        string          `json:"scopes,omitempty"`
	LaunchPatient string          `json:"launchPatient,omitempty"`
	PatientScope  string          `json:"patientScope,omitempty"`
	Tenant        string          `json:"tenant,omitempty"`
	PurposeOfUse  string          `json:"purposeOfUse,omitempty"`
	Action        string          `json:"action"`
	ResourceType  string          `json:"resourceType,omitempty"`
	ResourceID    string          `json:"resourceId,omitempty"`
	Resource      json.RawMessage `json:"resource,omitempty"`
	ToolName      string          `json:"toolName,omitempty"`
	Consent       json.RawMessage `json:"consent,omitempty"`
	ExpectAllow   bool            `json:"expectAllow"`
}

type declarativeFile struct {
	Scenarios []DeclarativeScenario `json:"scenarios"`
}

// LoadDeclarativeFile reads a JSON scenario fixture from disk.
func LoadDeclarativeFile(path string) ([]DeclarativeScenario, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file declarativeFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	return file.Scenarios, nil
}

// RunDeclarative executes portable scenarios that do not declare consent.
func RunDeclarative(ctx context.Context, scenarios []DeclarativeScenario) error {
	adapter := SmartAdapter()
	for _, sc := range scenarios {
		if len(sc.Consent) > 0 && string(sc.Consent) != "null" {
			continue
		}
		if err := runDeclarative(ctx, adapter, sc); err != nil {
			return fmt.Errorf("%s: %w", sc.ID, err)
		}
	}
	return nil
}

func runDeclarative(ctx context.Context, adapter *smart.AuthAdapter, sc DeclarativeScenario) error {
	cfg := BaseConfig()
	switch sc.Policy {
	case "narrow-observation":
		cfg.Policy = NarrowObservationPolicy()
	case "deny-first":
		cfg = NarrowEngineConfigWithDenyFirst()
	case "treat-only":
		cfg.Policy = treatOnlyPolicy()
	}
	if sc.Scopes != "" {
		// Research-layer overlay so wildcard SMART scopes can satisfy
		// RequiredPermissions. Not applied to policy-only scenarios.
		cfg = withWildcardRead(cfg)
	}
	eng, err := auth.NewEngine(cfg)
	if err != nil {
		return err
	}
	principal := RestrictedClinician()
	if sc.Principal == "admin" {
		principal = TenantAdmin()
	}
	tenant := TenantContextA()
	tenant.PatientScope = sc.PatientScope
	tenant.PurposeOfUse = sc.PurposeOfUse
	if sc.Tenant != "" {
		tenant.TenantID = sc.Tenant
	}

	var bundle smart.AuthBundle
	haveSMART := sc.Scopes != ""
	if haveSMART {
		scopes, err := smart.ParseScopes(sc.Scopes)
		if err != nil {
			return err
		}
		launchPatient := sc.LaunchPatient
		if launchPatient == "" {
			launchPatient = sc.PatientScope
		}
		bundle, err = adapter.ToAuthRequests(smart.TokenClaims{
			Subject: principal.ID,
			Scopes:  scopes,
			Scope:   sc.Scopes,
		}, smart.LaunchContext{PatientID: launchPatient, UserID: principal.ID})
		if err != nil {
			return err
		}
		principal = bundle.Principal
		tenant = bundle.Tenant
		if sc.Tenant != "" {
			tenant.TenantID = sc.Tenant
		}
		if sc.PurposeOfUse != "" {
			tenant.PurposeOfUse = sc.PurposeOfUse
		}
		if sc.PatientScope != "" {
			tenant.PatientScope = sc.PatientScope
		}
	}

	if d := eng.CheckTenantBinding(principal, tenant); !d.Allowed {
		return AssertDecision(sc.ID, sc.ExpectAllow, d, nil)
	}
	if d := eng.CheckPatientOverlay(tenant, overlayPatientID(sc)); !d.Allowed {
		return AssertDecision(sc.ID, sc.ExpectAllow, d, nil)
	}

	var decision auth.Decision
	switch sc.Action {
	case auth.ActionRead:
		if haveSMART && !adapter.ScopeImplies(bundle, sc.ResourceType, smart.VerbRead) {
			decision = auth.Deny("SMART scope does not grant " + sc.ResourceType + ".read")
			break
		}
		req := auth.ReadRequest{
			Principal: principal, Tenant: tenant, ResourceType: sc.ResourceType, ID: sc.ResourceID,
			PatientID: overlayPatientID(sc),
		}
		if haveSMART {
			req = adapter.ToReadRequest(bundle, sc.ResourceType, sc.ResourceID)
			req.Tenant = tenant
			req.Principal = principal
			req.PatientID = overlayPatientID(sc)
		}
		decision, err = eng.CanReadResource(ctx, req)
	case auth.ActionWrite:
		if haveSMART && !adapter.ScopeImplies(bundle, sc.ResourceType, smart.VerbWrite) {
			decision = auth.Deny("SMART scope does not grant " + sc.ResourceType + ".write")
			break
		}
		decision, err = eng.CanWriteResource(ctx, auth.WriteRequest{
			Principal: principal, Tenant: tenant, Operation: "update",
			ResourceType: sc.ResourceType, ID: sc.ResourceID, PatientID: overlayPatientID(sc),
		})
	case auth.ActionExecuteAITool:
		decision, err = eng.CanExecuteAITool(ctx, auth.AIToolRequest{Principal: principal, Tenant: tenant, ToolName: sc.ToolName})
	default:
		return fmt.Errorf("unsupported declarative action %q", sc.Action)
	}
	if err != nil {
		return err
	}
	return AssertDecision(sc.ID, sc.ExpectAllow, decision, nil)
}

// overlayPatientID is SEMANTICS gate 2: Patient.id or the patient reference
// on the scenario resource body (Observation.subject, Appointment.participant.actor).
func overlayPatientID(sc DeclarativeScenario) string {
	return auth.OverlayPatientID(sc.ResourceType, sc.ResourceID, auth.CompartmentPatientFromJSON(sc.ResourceType, sc.Resource))
}

// withWildcardRead adds *.read to the clinician role so wildcard SMART scopes
// can satisfy RequiredPermissions. Callers must apply it only for SMART cases.
func withWildcardRead(cfg auth.Config) auth.Config {
	roles := make([]auth.Role, len(cfg.Roles))
	copy(roles, cfg.Roles)
	for i := range roles {
		if roles[i].Name != "clinician" {
			continue
		}
		perms := append([]auth.Permission(nil), roles[i].Permissions...)
		perms = append(perms, "*.read")
		roles[i].Permissions = perms
	}
	cfg.Roles = roles
	return cfg
}

func treatOnlyPolicy() *auth.PolicyDocument {
	return &auth.PolicyDocument{
		Version: "1",
		Rules: []auth.PolicyRule{
			{
				Name:   "treat-obs",
				Effect: auth.EffectAllow,
				Match: auth.RuleMatch{
					Actions:        []string{auth.ActionRead},
					ResourceTypes:  []string{"Observation"},
					PurposeOfUse:   []string{"TREAT"},
					AnyPermissions: []string{"observation.read"},
				},
				Reason: "treat purpose only",
			},
			{
				Name:   "patient-access",
				Effect: auth.EffectAllow,
				Match:  auth.RuleMatch{Actions: []string{auth.ActionPatientAccess}},
				Reason: "patient compartment access",
			},
		},
	}
}

func findGoMod(start string) string {
	dir, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// DeclarativeFixturePath returns the authztest JSON compartment-overlay fixture.
func DeclarativeFixturePath() (string, error) {
	root := findGoMod(".")
	if root == "" {
		return "", fmt.Errorf("authztest: go.mod not found from working directory")
	}
	path := filepath.Join(root, "pkg", "testkit", "authztest", "testdata", "declarative-scenarios.json")
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}
