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

	decision, err := evaluateRequest(ctx, eng, adapter, bundle, haveSMART, principal, tenant, sc)
	if err != nil {
		return Outcome{}, err
	}

	if ok, reason := consentAllows(sc.Consent, sc); !ok {
		decision = auth.Deny(reason)
	}

	pass := decision.Allowed == sc.ExpectAllow
	return Outcome{
		ID:          sc.ID,
		Allowed:     decision.Allowed,
		ExpectAllow: sc.ExpectAllow,
		Reason:      decision.Reason,
		Pass:        pass,
	}, nil
}

func evaluateRequest(ctx context.Context, eng *auth.Engine, adapter *smart.AuthAdapter, bundle smart.AuthBundle, haveSMART bool, principal auth.Principal, tenant auth.TenantContext, sc Scenario) (auth.Decision, error) {
	switch sc.Action {
	case auth.ActionRead:
		if haveSMART && !adapter.ScopeImplies(bundle, sc.ResourceType, smart.VerbRead) {
			return auth.Deny(fmt.Sprintf("SMART scope does not grant %s.read", sc.ResourceType)), nil
		}
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

func consentAllows(c *ConsentState, sc Scenario) (bool, string) {
	if c == nil {
		return true, ""
	}
	if !strings.EqualFold(c.Status, "active") {
		return true, "inactive consent ignored"
	}
	patientID := sc.LaunchPatient
	if patientID == "" {
		patientID = sc.PatientScope
	}
	if sc.ResourceType == "Patient" {
		patientID = sc.ResourceID
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
	trueVal := true
	roles := []auth.Role{
		{
			Name: "clinician",
			Permissions: []auth.Permission{
				"appointment.read", "patient.read", "observation.read",
				"read-patient-summary", "*.read",
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
	principals := []auth.Principal{principalByName("clinician"), principalByName("admin")}
	return auth.Config{
		Roles:      roles,
		Principals: principals,
		Policy:     policyByName(sc.Policy, trueVal),
	}
}

func policyByName(name string, _ bool) *auth.PolicyDocument {
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
