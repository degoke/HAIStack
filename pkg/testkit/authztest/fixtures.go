package authztest

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

const (
	TenantA = "tenant-a"
	TenantB = "tenant-b"
)

// RestrictedClinician may read/write appointments only.
func RestrictedClinician() auth.Principal {
	return auth.Principal{
		ID:   "user-clinician",
		Kind: auth.KindUser,
		TenantBindings: []auth.TenantBinding{{
			TenantID: TenantA,
			Roles:    []string{"clinician"},
		}},
	}
}

// PatientScopedUser is a clinician bound to a single patient compartment.
func PatientScopedUser() auth.Principal {
	return RestrictedClinician()
}

// TenantAdmin may install modules and has broader permissions.
func TenantAdmin() auth.Principal {
	return auth.Principal{
		ID:   "user-admin",
		Kind: auth.KindUser,
		TenantBindings: []auth.TenantBinding{{
			TenantID: TenantA,
			Roles:    []string{"tenant-admin"},
		}},
	}
}

// BackendServicePrincipal represents a validated SMART backend client.
func BackendServicePrincipal() auth.Principal {
	return auth.Principal{
		ID:   "backend-app",
		Kind: auth.KindService,
		TenantBindings: []auth.TenantBinding{{
			TenantID: TenantA,
			Roles:    []string{"backend"},
		}},
	}
}

// TenantContextA is an unrestricted tenant binding.
func TenantContextA() auth.TenantContext {
	return auth.TenantContext{TenantID: TenantA}
}

// PatientScopedTenant scopes access to one patient id.
func PatientScopedTenant(patientID string) auth.TenantContext {
	return auth.TenantContext{
		TenantID:     TenantA,
		PatientScope: patientID,
		RoleBindings: []string{"clinician"},
	}
}

// BaseRoles returns the standard role catalog for scenario engines.
func BaseRoles() []auth.Role {
	return []auth.Role{
		{
			Name: "clinician",
			Permissions: []auth.Permission{
				"appointment.read",
				"read-patient-summary",
				"read-appointment",
				"patient.read",
				"observation.read",
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
		{
			Name:        "backend",
			Permissions: []auth.Permission{"*.read", "patient.read"},
		},
	}
}

// BasePolicy is a deny-by-default policy with appointment, view, AI, device, and module rules.
func BasePolicy() *auth.PolicyDocument {
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
				Reason: "patient compartment access",
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
	}
}

// NarrowObservationPolicy allows only Observation reads (policy narrows SMART patient/*.read).
func NarrowObservationPolicy() *auth.PolicyDocument {
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
			{
				Name:   "patient-access",
				Effect: auth.EffectAllow,
				Match: auth.RuleMatch{
					Actions: []string{auth.ActionPatientAccess},
				},
				Reason: "patient compartment access",
			},
		},
	}
}

// BaseDevices returns trusted and untrusted device fixtures.
func BaseDevices() []auth.DeviceIdentity {
	return []auth.DeviceIdentity{
		{
			DeviceID: "device-trusted",
			TenantID: TenantA,
			Status:   auth.DeviceStatusActive,
			Trusted:  true,
		},
		{
			DeviceID: "device-untrusted",
			TenantID: TenantA,
			Status:   auth.DeviceStatusActive,
			Trusted:  false,
		},
	}
}

// BaseConfig returns a standard engine configuration for scenario tests.
func BaseConfig() auth.Config {
	return auth.Config{
		Roles:      BaseRoles(),
		Principals: []auth.Principal{RestrictedClinician(), TenantAdmin(), BackendServicePrincipal()},
		Devices:    BaseDevices(),
		Policy:     BasePolicy(),
	}
}

// MustEngine constructs an auth.Engine or fails the test.
func MustEngine(t *testing.T, cfg auth.Config) *auth.Engine {
	t.Helper()
	eng, err := auth.NewEngine(cfg)
	if err != nil {
		t.Fatalf("authztest.MustEngine: %v", err)
	}
	return eng
}

// DefaultEngine returns an engine with BaseConfig.
func DefaultEngine(t *testing.T) *auth.Engine {
	return MustEngine(t, BaseConfig())
}

// SmartAdapter returns a configured SMART → auth adapter for scenario tests.
func SmartAdapter() *smart.AuthAdapter {
	return smart.NewAuthAdapter(smart.AuthAdapterConfig{
		DefaultTenantID:     TenantA,
		DefaultUserRoles:    []string{"clinician"},
		DefaultServiceRoles: []string{"backend"},
	})
}

func boolPtr(v bool) *bool { return &v }
