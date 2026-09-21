// Package policysemantics is the Track C vendor-neutral policy/consent catalogue.
package policysemantics

import (
	"context"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

const tenantA = "tenant-a"

// Outcome is one executed scenario.
type Outcome struct {
	ID          string `json:"id"`
	Allowed     bool   `json:"allowed"`
	ExpectAllow bool   `json:"expectAllow"`
	Reason      string `json:"reason"`
	Pass        bool   `json:"pass"`
}

// Report is the runner summary.
type Report struct {
	Total   int       `json:"total"`
	Passed  int       `json:"passed"`
	Failed  int       `json:"failed"`
	Results []Outcome `json:"results"`
}

// RunCatalog evaluates every scenario.
func RunCatalog(ctx context.Context, scenarios []Scenario) (Report, error) {
	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{
		DefaultTenantID:     tenantA,
		DefaultUserRoles:    []string{"clinician"},
		DefaultServiceRoles: []string{"backend"},
	})
	var report Report
	report.Total = len(scenarios)
	for _, sc := range scenarios {
		out, err := runOne(ctx, adapter, sc)
		if err != nil {
			return report, fmt.Errorf("%s: %w", sc.ID, err)
		}
		if out.Pass {
			report.Passed++
		} else {
			report.Failed++
		}
		report.Results = append(report.Results, out)
	}
	return report, nil
}

func runOne(ctx context.Context, adapter *smart.AuthAdapter, sc Scenario) (Outcome, error) {
	eng, err := auth.NewEngine(engineConfig(sc))
	if err != nil {
		return Outcome{}, err
	}
	principal := principalByName(sc.Principal)
	tenant := auth.TenantContext{
		TenantID:     tenantA,
		PurposeOfUse: sc.PurposeOfUse,
		PatientScope: sc.PatientScope,
	}
	if sc.Tenant != "" {
		tenant.TenantID = sc.Tenant
	}

	var bundle smart.AuthBundle
	var haveSMART bool
	if sc.Scopes != "" {
		haveSMART = true
		scopes, err := smart.ParseScopes(sc.Scopes)
		if err != nil {
			return Outcome{}, err
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
			return Outcome{}, err
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
		return outcomeFrom(sc, d), nil
	}
	if d := eng.CheckPatientOverlay(tenant, OverlayPatientID(sc)); !d.Allowed {
		return outcomeFrom(sc, d), nil
	}
	if haveSMART {
		if verb, ok := smartVerb(sc.Action); ok && !adapter.ScopeImplies(bundle, sc.ResourceType, verb) {
			return outcomeFrom(sc, auth.Deny(fmt.Sprintf("SMART scope does not grant %s.%s", sc.ResourceType, verb))), nil
		}
	}
	if ok, reason := consentAllows(sc.Consent, sc); !ok {
		return outcomeFrom(sc, auth.Deny(reason)), nil
	}

	decision, err := evaluateRequest(ctx, eng, adapter, bundle, haveSMART, principal, tenant, sc)
	if err != nil {
		return Outcome{}, err
	}
	return outcomeFrom(sc, decision), nil
}

func evaluateRequest(ctx context.Context, eng *auth.Engine, adapter *smart.AuthAdapter, bundle smart.AuthBundle, haveSMART bool, principal auth.Principal, tenant auth.TenantContext, sc Scenario) (auth.Decision, error) {
	switch sc.Action {
	case auth.ActionRead:
		req := auth.ReadRequest{
			Principal:    principal,
			Tenant:       tenant,
			ResourceType: sc.ResourceType,
			ID:           sc.ResourceID,
		}
		if haveSMART {
			req = adapter.ToReadRequest(bundle, sc.ResourceType, sc.ResourceID)
			req.Tenant = tenant
			req.Principal = principal
		}
		return eng.CanReadResource(ctx, req)
	case auth.ActionWrite:
		req := auth.WriteRequest{
			Principal:    principal,
			Tenant:       tenant,
			Operation:    "update",
			ResourceType: sc.ResourceType,
			ID:           sc.ResourceID,
		}
		return eng.CanWriteResource(ctx, req)
	case auth.ActionExecuteAITool:
		return eng.CanExecuteAITool(ctx, auth.AIToolRequest{
			Principal: principal,
			Tenant:    tenant,
			ToolName:  sc.ToolName,
		})
	case auth.ActionExecuteView:
		return eng.CanExecuteView(ctx, auth.ViewRequest{
			Principal: principal,
			Tenant:    tenant,
			ViewName:  sc.ResourceID,
		})
	default:
		return auth.Decision{}, fmt.Errorf("unknown action %q", sc.Action)
	}
}

func outcomeFrom(sc Scenario, decision auth.Decision) Outcome {
	return Outcome{
		ID:          sc.ID,
		Allowed:     decision.Allowed,
		ExpectAllow: sc.ExpectAllow,
		Reason:      decision.Reason,
		Pass:        decision.Allowed == sc.ExpectAllow,
	}
}

func smartVerb(action string) (smart.AccessVerb, bool) {
	switch action {
	case auth.ActionRead:
		return smart.VerbRead, true
	case auth.ActionWrite:
		return smart.VerbWrite, true
	default:
		return "", false
	}
}

// OverlayPatientID is SEMANTICS gate 2's target patient: Patient.id, else the
// authored compartment patient, else the SMART launch patient.
func OverlayPatientID(sc Scenario) string {
	if strings.EqualFold(sc.ResourceType, "Patient") {
		return sc.ResourceID
	}
	if sc.CompartmentPatient != "" {
		return sc.CompartmentPatient
	}
	return sc.LaunchPatient
}

func consentAllows(c *ConsentState, sc Scenario) (bool, string) {
	if c == nil {
		return true, ""
	}
	if !strings.EqualFold(c.Status, "active") {
		return true, "inactive consent ignored"
	}
	patientID := OverlayPatientID(sc)
	if patientID == "" {
		patientID = sc.PatientScope
	}
	if c.PatientID != "" && patientID != "" && c.PatientID != patientID {
		return true, "consent for another patient"
	}
	if len(c.ResourceTypes) > 0 && !containsFold(c.ResourceTypes, sc.ResourceType) {
		return true, "consent class does not apply"
	}
	if len(c.Actions) > 0 {
		actionMatches := containsFold(c.Actions, sc.Action) || (sc.Action == "read" && containsFold(c.Actions, "access"))
		if !actionMatches {
			return true, "consent action does not apply"
		}
	}
	if strings.EqualFold(c.ProvisionType, "deny") {
		return false, "consent deny provision"
	}
	return true, "consent permit"
}

func containsFold(list []string, want string) bool {
	for _, item := range list {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}

func principalByName(name string) auth.Principal {
	switch name {
	case "admin":
		return auth.Principal{
			ID:   "user-admin",
			Kind: auth.KindUser,
			TenantBindings: []auth.TenantBinding{{
				TenantID: tenantA,
				Roles:    []string{"tenant-admin"},
			}},
		}
	default:
		return auth.Principal{
			ID:   "user-clinician",
			Kind: auth.KindUser,
			TenantBindings: []auth.TenantBinding{{
				TenantID: tenantA,
				Roles:    []string{"clinician"},
			}},
		}
	}
}

func engineConfig(sc Scenario) auth.Config {
	roles := []auth.Role{
		{
			Name: "clinician",
			Permissions: []auth.Permission{
				"appointment.read", "patient.read", "observation.read",
				"read-patient-summary",
			},
		},
		{
			Name:        "tenant-admin",
			Permissions: []auth.Permission{"module.install", "appointment.read", "patient.read"},
		},
		{
			Name:        "backend",
			Permissions: []auth.Permission{"*.read", "patient.read"},
		},
	}
	if sc.Scopes != "" {
		// Research overlay: wildcard SMART scopes set RequiredPermissions to
		// *.read. Production pkg/auth ∩ SMART does not inject this permission.
		roles = withWildcardRead(roles)
	}
	principals := []auth.Principal{principalByName("clinician"), principalByName("admin")}
	return auth.Config{
		Roles:      roles,
		Principals: principals,
		Policy:     policyByName(sc.Policy),
	}
}

func withWildcardRead(roles []auth.Role) []auth.Role {
	out := make([]auth.Role, len(roles))
	copy(out, roles)
	for i := range out {
		if out[i].Name != "clinician" {
			continue
		}
		perms := append([]auth.Permission(nil), out[i].Permissions...)
		perms = append(perms, "*.read")
		out[i].Permissions = perms
	}
	return out
}

func policyByName(name string) *auth.PolicyDocument {
	patientAccess := auth.PolicyRule{
		Name:   "patient-access",
		Effect: auth.EffectAllow,
		Match:  auth.RuleMatch{Actions: []string{auth.ActionPatientAccess}},
		Reason: "patient compartment access",
	}
	switch name {
	case "deny-first":
		return &auth.PolicyDocument{
			Version: "1",
			Rules: []auth.PolicyRule{
				{
					Name:   "deny-appointment",
					Effect: auth.EffectDeny,
					Match:  auth.RuleMatch{Actions: []string{auth.ActionRead}, ResourceTypes: []string{"Appointment"}},
					Reason: "explicit deny first",
				},
				{
					Name:   "allow-appointment",
					Effect: auth.EffectAllow,
					Match:  auth.RuleMatch{Actions: []string{auth.ActionRead}, ResourceTypes: []string{"Appointment"}},
					Reason: "later allow",
				},
				patientAccess,
			},
		}
	case "narrow-observation":
		return &auth.PolicyDocument{
			Version: "1",
			Rules: []auth.PolicyRule{
				{
					Name:   "observation-only",
					Effect: auth.EffectAllow,
					Match: auth.RuleMatch{
						Actions:        []string{auth.ActionRead},
						ResourceTypes:  []string{"Observation"},
						AnyPermissions: []string{"observation.read", "*.read"},
					},
					Reason: "policy allows observation only",
				},
				patientAccess,
			},
		}
	case "treat-only":
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
				patientAccess,
			},
		}
	default:
		return &auth.PolicyDocument{
			Version: "1",
			Rules: []auth.PolicyRule{
				{
					Name:   "appointment-rw",
					Effect: auth.EffectAllow,
					Match: auth.RuleMatch{
						Actions:        []string{auth.ActionRead, auth.ActionWrite},
						ResourceTypes:  []string{"Appointment"},
						AnyPermissions: []string{"appointment.read"},
					},
					Reason: "clinicians may access appointments",
				},
				{
					Name:   "patient-read",
					Effect: auth.EffectAllow,
					Match: auth.RuleMatch{
						Actions:        []string{auth.ActionRead},
						ResourceTypes:  []string{"Patient"},
						AnyPermissions: []string{"patient.read"},
					},
					Reason: "patient read allowed",
				},
				{
					Name:   "ai-run-view",
					Effect: auth.EffectAllow,
					Match: auth.RuleMatch{
						Actions:   []string{auth.ActionExecuteAITool},
						ToolNames: []string{"run_view"},
					},
					Reason: "ai may run view tool",
				},
				patientAccess,
			},
		}
	}
}
