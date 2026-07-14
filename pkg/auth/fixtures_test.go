package auth_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/auth"
)

func clinician() auth.Principal {
	return auth.Principal{
		ID:   "user-1",
		Kind: auth.KindUser,
		TenantBindings: []auth.TenantBinding{{
			TenantID: "tenant-a",
			Roles:    []string{"clinician"},
		}},
	}
}

func admin() auth.Principal {
	return auth.Principal{
		ID:   "admin-1",
		Kind: auth.KindUser,
		TenantBindings: []auth.TenantBinding{{
			TenantID: "tenant-a",
			Roles:    []string{"tenant-admin"},
		}},
	}
}

func tenantA() auth.TenantContext {
	return auth.TenantContext{TenantID: "tenant-a"}
}

func baseConfig() auth.Config {
	return auth.Config{
		Roles: []auth.Role{
			{
				Name: "clinician",
				Permissions: []auth.Permission{
					"appointment.read",
					"read-patient-summary",
					"read-appointment",
				},
			},
			{
				Name: "tenant-admin",
				Permissions: []auth.Permission{
					"module.install",
					"read-appointment",
					"appointment.read",
					"read-patient-summary",
				},
			},
		},
		Principals: []auth.Principal{clinician(), admin()},
		Devices: []auth.DeviceIdentity{{
			DeviceID: "device-1",
			TenantID: "tenant-a",
			Status:   auth.DeviceStatusActive,
			Trusted:  true,
		}},
		Policy: &auth.PolicyDocument{
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
					Name:   "patient-summary-view",
					Effect: auth.EffectAllow,
					Match: auth.RuleMatch{
						Actions:   []string{auth.ActionExecuteView},
						ViewNames: []string{"patient_summary_view"},
					},
					Reason: "view allowed",
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
				{
					Name:   "patient-access",
					Effect: auth.EffectAllow,
					Match: auth.RuleMatch{
						Actions: []string{auth.ActionPatientAccess},
					},
					Reason: "patient access stub",
				},
				{
					Name:   "device-push",
					Effect: auth.EffectAllow,
					Match: auth.RuleMatch{
						Actions:        []string{auth.ActionPushDevice},
						DeviceTrusted:  boolPtr(true),
						DeviceStatuses: []string{auth.DeviceStatusActive},
					},
					Reason: "trusted device may push",
				},
				{
					Name:   "install-scheduling",
					Effect: auth.EffectAllow,
					Match: auth.RuleMatch{
						Actions:     []string{auth.ActionInstallModule},
						ModuleNames: []string{"scheduling"},
						Roles:       []string{"tenant-admin"},
					},
					Reason: "admin may install scheduling",
				},
			},
		},
	}
}

func mustEngine(t *testing.T, cfg auth.Config) *auth.Engine {
	t.Helper()
	eng, err := auth.NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return eng
}

func boolPtr(v bool) *bool { return &v }
