package auth_test

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/ai"
	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/modules"
	"github.com/degoke/health-ai-stack/pkg/view"
)

func TestDenyByDefault(t *testing.T) {
	eng := mustEngine(t, auth.Config{
		Roles:      []auth.Role{{Name: "clinician", Permissions: []auth.Permission{"appointment.read"}}},
		Principals: []auth.Principal{clinician()},
		Policy: &auth.PolicyDocument{
			Version: "1",
			Rules: []auth.PolicyRule{{
				Name:   "noop-deny",
				Effect: auth.EffectDeny,
				Match:  auth.RuleMatch{Actions: []string{"never"}},
				Reason: "explicit deny never",
			}},
		},
	})
	d, err := eng.CanReadResource(context.Background(), auth.ReadRequest{
		Principal:    clinician(),
		Tenant:       tenantA(),
		ResourceType: "Appointment",
		ID:           "a1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed {
		t.Fatalf("expected deny, got %#v", d)
	}
	if d.Reason == "" {
		t.Fatal("expected deny reason")
	}
}

func TestPrincipalRolePermissionEvaluation(t *testing.T) {
	eng := mustEngine(t, baseConfig())
	d, err := eng.CanReadResource(context.Background(), auth.ReadRequest{
		Principal:    clinician(),
		Tenant:       tenantA(),
		ResourceType: "Appointment",
		ID:           "a1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed {
		t.Fatalf("expected allow, got %#v", d)
	}
}

func TestTenantScopedAllowDeny(t *testing.T) {
	eng := mustEngine(t, baseConfig())
	d, err := eng.CanReadResource(context.Background(), auth.ReadRequest{
		Principal:    clinician(),
		Tenant:       auth.TenantContext{TenantID: "tenant-b"},
		ResourceType: "Appointment",
		ID:           "a1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed {
		t.Fatalf("expected deny for unbound tenant, got %#v", d)
	}
}

func TestWriteAppointmentAllowed(t *testing.T) {
	eng := mustEngine(t, baseConfig())
	d, err := eng.CanWriteResource(context.Background(), auth.WriteRequest{
		Principal:    clinician(),
		Tenant:       tenantA(),
		Operation:    "update",
		ResourceType: "Appointment",
		ID:           "a1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed {
		t.Fatalf("expected allow write, got %#v", d)
	}
}

func TestDevicePushTrustedSameTenant(t *testing.T) {
	eng := mustEngine(t, baseConfig())
	d, err := eng.CanPushDeviceEvent(context.Background(), auth.DevicePushRequest{
		DeviceID: "device-1",
		TenantID: "tenant-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed {
		t.Fatalf("expected allow, got %#v", d)
	}
}

func TestDevicePushWrongTenant(t *testing.T) {
	eng := mustEngine(t, baseConfig())
	d, err := eng.CanPushDeviceEvent(context.Background(), auth.DevicePushRequest{
		DeviceID: "device-1",
		TenantID: "tenant-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed {
		t.Fatalf("expected deny, got %#v", d)
	}
}

func TestDevicePushUntrusted(t *testing.T) {
	cfg := baseConfig()
	cfg.Devices = []auth.DeviceIdentity{{
		DeviceID: "device-2",
		TenantID: "tenant-a",
		Status:   auth.DeviceStatusActive,
		Trusted:  false,
	}}
	eng := mustEngine(t, cfg)
	d, err := eng.CanPushDeviceEvent(context.Background(), auth.DevicePushRequest{
		DeviceID: "device-2",
		TenantID: "tenant-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed {
		t.Fatalf("expected deny for untrusted device, got %#v", d)
	}
}

func TestModuleInstallAllowed(t *testing.T) {
	eng := mustEngine(t, baseConfig())
	d, err := eng.CanInstallModule(context.Background(), auth.ModuleInstallRequest{
		Principal:           admin(),
		Tenant:              tenantA(),
		ModuleName:          "scheduling",
		ModuleVersion:       "1.0.0",
		RequiredPermissions: []string{"read-appointment"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed {
		t.Fatalf("expected allow, got %#v", d)
	}
}

func TestModuleInstallMissingPermission(t *testing.T) {
	eng := mustEngine(t, baseConfig())
	d, err := eng.CanInstallModule(context.Background(), auth.ModuleInstallRequest{
		Principal:           clinician(),
		Tenant:              tenantA(),
		ModuleName:          "scheduling",
		RequiredPermissions: []string{"module.install"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed {
		t.Fatalf("expected deny missing module.install, got %#v", d)
	}
}

func TestModuleInstallPolicyDenied(t *testing.T) {
	eng := mustEngine(t, baseConfig())
	d, err := eng.CanInstallModule(context.Background(), auth.ModuleInstallRequest{
		Principal:     admin(),
		Tenant:        tenantA(),
		ModuleName:    "forbidden-mod",
		ModuleVersion: "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed {
		t.Fatalf("expected deny for module not in policy, got %#v", d)
	}
}

func TestViewAuthorizerAdapter(t *testing.T) {
	eng := mustEngine(t, baseConfig())
	authorizer := &auth.ViewAuthorizer{
		Engine:   eng,
		TenantID: "tenant-a",
		Resolve: func(_ context.Context, actor, _ string) (auth.Principal, auth.TenantContext, error) {
			p, err := eng.Catalog().GetPrincipal(actor)
			return p, auth.TenantContext{TenantID: "tenant-a"}, err
		},
	}
	err := authorizer.AuthorizeView(context.Background(), view.AuthRequest{
		ViewName:     "patient_summary_view",
		ResourceType: "Patient",
		Actor:        "user-1",
		Permissions:  []string{"read-patient-summary"},
	})
	if err != nil {
		t.Fatalf("AuthorizeView: %v", err)
	}

	err = authorizer.AuthorizeView(context.Background(), view.AuthRequest{
		ViewName:     "patient_summary_view",
		ResourceType: "Patient",
		Actor:        "user-1",
		Permissions:  []string{"missing-permission"},
	})
	if !errors.Is(err, view.ErrUnauthorized) {
		t.Fatalf("err = %v, want view.ErrUnauthorized", err)
	}
}

func TestAIPolicyAdapter_ReadSearchViewWrite(t *testing.T) {
	eng := mustEngine(t, baseConfig())
	resolve := func(_ context.Context, actor, _ string) (auth.Principal, auth.TenantContext, error) {
		p, err := eng.Catalog().GetPrincipal(actor)
		return p, auth.TenantContext{TenantID: "tenant-a"}, err
	}
	adapter := &auth.AIPolicyAdapter{
		Engine:   eng,
		TenantID: "tenant-a",
		Resolve:  resolve,
		Constraints: &auth.AIConstraints{
			Search: map[string]ai.SearchTypePolicy{
				"Appointment": {AllowedParams: []string{"date", "patient"}, MaxCount: 25},
			},
			Views: map[string]ai.ViewTypePolicy{
				"patient_summary_view": {Deidentify: true},
			},
			Write: map[string]ai.WriteTypePolicy{
				"Appointment": {
					CreateFields: []string{"status", "start"},
					UpdateFields: []string{"status"},
				},
			},
		},
	}

	read, err := adapter.CheckRead(context.Background(), ai.ReadPolicyRequest{
		Actor: "user-1", ResourceType: "Appointment", ID: "a1",
	})
	if err != nil || !read.Allowed {
		t.Fatalf("CheckRead = %#v err=%v", read, err)
	}

	search, err := adapter.CheckSearch(context.Background(), ai.SearchPolicyRequest{
		Actor: "user-1", ResourceType: "Appointment",
		Params: url.Values{"date": {"2026-01-01"}, "identifier": {"x"}},
	})
	if err == nil || search.Allowed || !errors.Is(err, ai.ErrPolicyDenied) {
		t.Fatalf("CheckSearch = %#v err=%v, want mixed-parameter denial", search, err)
	}

	if err := adapter.CheckView(context.Background(), ai.ViewPolicyRequest{
		Actor: "user-1", ViewName: "patient_summary_view",
	}); err != nil {
		t.Fatalf("CheckView: %v", err)
	}
	viewDecision, err := adapter.CheckViewDecision(ai.ViewPolicyRequest{
		Actor: "user-1", ViewName: "patient_summary_view",
	})
	if err != nil || !viewDecision.Allowed || !viewDecision.Deidentify {
		t.Fatalf("CheckViewDecision = %#v err=%v", viewDecision, err)
	}
	if max, ok := adapter.MaxSearchCount("Appointment"); !ok || max != 25 {
		t.Fatalf("MaxSearchCount = (%d, %v), want (25, true)", max, ok)
	}

	write, err := adapter.CheckWrite(context.Background(), ai.WritePolicyRequest{
		Actor: "user-1", Operation: "update", ResourceType: "Appointment", ID: "a1",
		Fields: map[string]any{"status": "booked"},
	})
	if err != nil || !write.Allowed {
		t.Fatalf("CheckWrite = %#v err=%v", write, err)
	}

	_, err = adapter.CheckWrite(context.Background(), ai.WritePolicyRequest{
		Actor: "user-1", Operation: "update", ResourceType: "Appointment", ID: "a1",
		Fields: map[string]any{"comment": "nope"},
	})
	if !errors.Is(err, ai.ErrPolicyDenied) {
		t.Fatalf("err = %v, want ErrPolicyDenied", err)
	}
}

func TestAIPolicyAdapter_EnforcesPatientScopeForSearchAndViews(t *testing.T) {
	cfg := baseConfig()
	cfg.Roles[0].Permissions = append(cfg.Roles[0].Permissions, "patient.read")
	cfg.Policy.Rules = append(cfg.Policy.Rules, auth.PolicyRule{
		Name:   "patient-read",
		Effect: auth.EffectAllow,
		Match: auth.RuleMatch{
			Actions:        []string{auth.ActionRead},
			ResourceTypes:  []string{"Patient"},
			AnyPermissions: []string{"patient.read"},
		},
	})
	eng := mustEngine(t, cfg)
	resolve := func(_ context.Context, actor, _ string) (auth.Principal, auth.TenantContext, error) {
		p, err := eng.Catalog().GetPrincipal(actor)
		return p, auth.TenantContext{TenantID: "tenant-a", PatientScope: "pat-1"}, err
	}
	adapter := &auth.AIPolicyAdapter{
		Engine: eng, Resolve: resolve, TenantID: "tenant-a",
		PatientSearchParams: auth.MapPatientSearchParamResolver{"Appointment": "patient"},
		Constraints: &auth.AIConstraints{Search: map[string]ai.SearchTypePolicy{
			"Patient":     {AllowedParams: []string{"name"}},
			"Appointment": {AllowedParams: []string{"date", "patient"}},
		}},
	}

	patientSearch, err := adapter.CheckSearch(context.Background(), ai.SearchPolicyRequest{
		Actor: "user-1", ResourceType: "Patient", Params: url.Values{"name": {"Doe"}},
	})
	if err != nil {
		t.Fatalf("patient search: %v", err)
	}
	if got := patientSearch.Params.Get("_id"); got != "pat-1" {
		t.Fatalf("patient scope filter = %q, want pat-1", got)
	}

	appointmentSearch, err := adapter.CheckSearch(context.Background(), ai.SearchPolicyRequest{
		Actor: "user-1", ResourceType: "Appointment", Params: url.Values{"date": {"2026-01-01"}},
	})
	if err != nil {
		t.Fatalf("appointment search: %v", err)
	}
	if got := appointmentSearch.Params.Get("patient"); got != "Patient/pat-1" {
		t.Fatalf("appointment scope filter = %q, want Patient/pat-1", got)
	}

	if _, err := adapter.ApplyViewScope(context.Background(), ai.ViewPolicyRequest{
		Actor: "user-1", ViewName: "patient_summary_view",
	}); !errors.Is(err, ai.ErrPolicyDenied) {
		t.Fatalf("scoped view without enforcer err = %v, want policy denial", err)
	}
	adapter.PatientViewScope = func(_ context.Context, _ ai.ViewPolicyRequest, patientID string) (map[string]any, error) {
		return map[string]any{"patientId": patientID}, nil
	}
	narrowed, err := adapter.ApplyViewScope(context.Background(), ai.ViewPolicyRequest{
		Actor: "user-1", ViewName: "patient_summary_view",
	})
	if err != nil {
		t.Fatalf("scoped view: %v", err)
	}
	if narrowed.Parameters["patientId"] != "pat-1" {
		t.Fatalf("view scope parameters = %#v", narrowed.Parameters)
	}
}

func TestPatientScopeStub(t *testing.T) {
	eng := mustEngine(t, baseConfig())
	scoped := auth.TenantContext{TenantID: "tenant-a", PatientScope: "pat-1", RoleBindings: []string{"clinician"}}

	d, err := eng.CheckPatientScope(context.Background(), auth.PatientScopeRequest{
		Principal: clinician(),
		Tenant:    scoped,
		PatientID: "pat-1",
	})
	if err != nil || !d.Allowed {
		t.Fatalf("expected allow matching scope, got %#v err=%v", d, err)
	}

	d, err = eng.CheckPatientScope(context.Background(), auth.PatientScopeRequest{
		Principal: clinician(),
		Tenant:    scoped,
		PatientID: "pat-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed {
		t.Fatalf("expected deny other patient, got %#v", d)
	}

	// Resource read of Patient outside scope is denied by stub gate.
	d, err = eng.CanReadResource(context.Background(), auth.ReadRequest{
		Principal:    clinician(),
		Tenant:       scoped,
		ResourceType: "Patient",
		ID:           "pat-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed {
		t.Fatalf("expected patient stub deny, got %#v", d)
	}
}

func TestDSLParseCompileEvaluate(t *testing.T) {
	yamlDoc := `
version: "1"
rules:
  - name: allow-read
    effect: allow
    match:
      actions: [read]
      resourceTypes: [Observation]
      anyPermissions: [observation.read]
    reason: allow observation read
`
	compiled, err := auth.ParseAndCompilePolicy([]byte(yamlDoc), auth.PolicyFormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	eng := mustEngine(t, auth.Config{
		Roles: []auth.Role{{Name: "r", Permissions: []auth.Permission{"observation.read"}}},
		Principals: []auth.Principal{{
			ID: "p", Kind: auth.KindUser,
			TenantBindings: []auth.TenantBinding{{TenantID: "t", Roles: []string{"r"}}},
		}},
		Compiled: compiled,
	})
	d, err := eng.CanReadResource(context.Background(), auth.ReadRequest{
		Principal: auth.Principal{
			ID: "p", Kind: auth.KindUser,
			TenantBindings: []auth.TenantBinding{{TenantID: "t", Roles: []string{"r"}}},
		},
		Tenant:       auth.TenantContext{TenantID: "t"},
		ResourceType: "Observation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed {
		t.Fatalf("expected allow, got %#v", d)
	}
	if d.Reason != "allow observation read" {
		t.Fatalf("reason = %q", d.Reason)
	}

	_, err = auth.CompilePolicy(auth.PolicyDocument{Version: "1"})
	if !errors.Is(err, auth.ErrInvalidPolicy) {
		t.Fatalf("err = %v, want ErrInvalidPolicy", err)
	}
}

func TestPermissionEquivalence(t *testing.T) {
	eng := mustEngine(t, auth.Config{
		Roles: []auth.Role{{
			Name:        "clinician",
			Permissions: []auth.Permission{"read-appointment"},
		}},
		Principals: []auth.Principal{clinician()},
		Policy: &auth.PolicyDocument{
			Version: "1",
			Rules: []auth.PolicyRule{{
				Name:   "appt",
				Effect: auth.EffectAllow,
				Match: auth.RuleMatch{
					Actions:        []string{auth.ActionRead},
					ResourceTypes:  []string{"Appointment"},
					AnyPermissions: []string{"appointment.read"},
				},
			}},
		},
	})
	d, err := eng.CanReadResource(context.Background(), auth.ReadRequest{
		Principal: clinician(), Tenant: tenantA(), ResourceType: "Appointment",
	})
	if err != nil || !d.Allowed {
		t.Fatalf("expected equivalent permission allow, got %#v err=%v", d, err)
	}
}

func TestCanExecuteAITool(t *testing.T) {
	eng := mustEngine(t, baseConfig())
	d, err := eng.CanExecuteAITool(context.Background(), auth.AIToolRequest{
		Principal: clinician(),
		Tenant:    tenantA(),
		ToolName:  ai.ToolRunView,
		ViewName:  "patient_summary_view",
	})
	if err != nil || !d.Allowed {
		t.Fatalf("expected allow, got %#v err=%v", d, err)
	}
}

func TestModuleInstallerAuthorizer(t *testing.T) {
	eng := mustEngine(t, baseConfig())
	authorizer := &auth.ModuleInstallerAuthorizer{
		Engine: eng,
		Resolve: func(_ context.Context) (auth.Principal, auth.TenantContext, error) {
			return admin(), tenantA(), nil
		},
	}
	err := authorizer.AuthorizeModuleInstall(context.Background(), modules.InstallAuthRequest{
		Path: filepath.Join("..", "..", "modules", "scheduling"),
		Module: modules.Module{
			Manifest: modules.Manifest{
				Name:        "scheduling",
				Version:     "1.0.0",
				Permissions: []string{"read-appointment"},
			},
		},
		Plan: &modules.Plan{Name: "scheduling", Version: "1.0.0", Action: "install"},
	})
	if err != nil {
		t.Fatalf("AuthorizeModuleInstall: %v", err)
	}

	authorizer.Resolve = func(_ context.Context) (auth.Principal, auth.TenantContext, error) {
		return clinician(), tenantA(), nil
	}
	err = authorizer.AuthorizeModuleInstall(context.Background(), modules.InstallAuthRequest{
		Module: modules.Module{
			Manifest: modules.Manifest{
				Name:        "scheduling",
				Version:     "1.0.0",
				Permissions: []string{"read-appointment"},
			},
		},
		Plan: &modules.Plan{Name: "scheduling", Version: "1.0.0", Action: "install"},
	})
	if !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("err = %v, want auth.ErrDenied", err)
	}
}

func TestDecisionReasonsOnAllowAndDeny(t *testing.T) {
	eng := mustEngine(t, baseConfig())
	allow, err := eng.CanReadResource(context.Background(), auth.ReadRequest{
		Principal: clinician(), Tenant: tenantA(), ResourceType: "Appointment",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !allow.Allowed || allow.Reason == "" {
		t.Fatalf("allow decision = %#v", allow)
	}
	deny, err := eng.CanReadResource(context.Background(), auth.ReadRequest{
		Principal: clinician(), Tenant: tenantA(), ResourceType: "Observation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if deny.Allowed || deny.Reason == "" {
		t.Fatalf("deny decision = %#v", deny)
	}
}

func TestNewEngineRequiresPolicy(t *testing.T) {
	_, err := auth.NewEngine(auth.Config{})
	if !errors.Is(err, auth.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestOrderedDenyWinsBeforeAllow(t *testing.T) {
	eng := mustEngine(t, auth.Config{
		Roles:      []auth.Role{{Name: "clinician", Permissions: []auth.Permission{"appointment.read"}}},
		Principals: []auth.Principal{clinician()},
		Policy: &auth.PolicyDocument{
			Version: "1",
			Rules: []auth.PolicyRule{
				{
					Name:   "deny-first",
					Effect: auth.EffectDeny,
					Match:  auth.RuleMatch{Actions: []string{auth.ActionRead}, ResourceTypes: []string{"Appointment"}},
					Reason: "blocked",
				},
				{
					Name:   "allow-second",
					Effect: auth.EffectAllow,
					Match:  auth.RuleMatch{Actions: []string{auth.ActionRead}, ResourceTypes: []string{"Appointment"}},
					Reason: "would allow",
				},
			},
		},
	})
	d, err := eng.CanReadResource(context.Background(), auth.ReadRequest{
		Principal: clinician(), Tenant: tenantA(), ResourceType: "Appointment",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed || d.Reason != "blocked" {
		t.Fatalf("decision = %#v", d)
	}
}
