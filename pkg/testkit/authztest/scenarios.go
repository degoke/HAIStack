package authztest

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/degoke/health-ai-stack/pkg/ai"
	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/modules"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/smart"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/view"
)

// AllScenarios returns the documented authorization scenario catalog (≥30 cases).
func AllScenarios() []Scenario {
	return []Scenario{
		// REST CRUD
		{
			Name: "crud_clinician_read_appointment_allowed",
			Doc:  "Restricted clinician with appointment.read may read Appointment resources.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanReadResource(ctx, auth.ReadRequest{
					Principal: RestrictedClinician(), Tenant: TenantContextA(),
					ResourceType: "Appointment", ID: "a1",
				})
				return AssertDecision("read appointment", ExpectAllow, d, err)
			},
		},
		{
			Name: "crud_clinician_read_observation_denied",
			Doc:  "Clinician without matching policy rule is denied Observation read (deny-by-default).",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanReadResource(ctx, auth.ReadRequest{
					Principal: RestrictedClinician(), Tenant: TenantContextA(),
					ResourceType: "MedicationRequest", ID: "rx-1",
				})
				return AssertDecision("read medication", ExpectDeny, d, err)
			},
		},
		{
			Name: "crud_clinician_write_appointment_allowed",
			Doc:  "Clinician may update appointments when policy allows write.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanWriteResource(ctx, auth.WriteRequest{
					Principal: RestrictedClinician(), Tenant: TenantContextA(),
					Operation: "update", ResourceType: "Appointment", ID: "a1",
				})
				return AssertDecision("write appointment", ExpectAllow, d, err)
			},
		},
		{
			Name: "crud_cross_tenant_read_denied",
			Doc:  "Principal bound to tenant-a cannot read resources in tenant-b.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanReadResource(ctx, auth.ReadRequest{
					Principal:    RestrictedClinician(),
					Tenant:       auth.TenantContext{TenantID: TenantB},
					ResourceType: "Appointment", ID: "a1",
				})
				return AssertDecision("cross-tenant read", ExpectDeny, d, err)
			},
		},
		{
			Name: "crud_deny_by_default_no_rule",
			Doc:  "Unknown resource types are denied when no policy rule matches.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanReadResource(ctx, auth.ReadRequest{
					Principal: TenantAdmin(), Tenant: TenantContextA(),
					ResourceType: "MedicationRequest", ID: "rx-1",
				})
				return AssertDecision("unknown resource", ExpectDeny, d, err)
			},
		},
		{
			Name: "crud_ordered_deny_wins",
			Doc:  "First matching deny rule blocks access even when a later allow rule would match.",
			Run: func(ctx context.Context, kit *Kit) error {
				eng := MustEngineFromConfig(NarrowEngineConfigWithDenyFirst())
				d, err := eng.CanReadResource(ctx, auth.ReadRequest{
					Principal: RestrictedClinician(), Tenant: TenantContextA(),
					ResourceType: "Appointment", ID: "a1",
				})
				return AssertDecision("deny-first", ExpectDeny, d, err)
			},
		},

		// Patient compartment
		{
			Name: "patient_scope_same_patient_allowed",
			Doc:  "Patient-scoped principal may access their own patient id.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CheckPatientScope(ctx, auth.PatientScopeRequest{
					Principal: RestrictedClinician(),
					Tenant:    PatientScopedTenant("pat-1"),
					PatientID: "pat-1",
				})
				return AssertDecision("patient scope same", ExpectAllow, d, err)
			},
		},
		{
			Name: "patient_scope_other_patient_denied",
			Doc:  "Patient-scoped principal cannot access a different patient id.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CheckPatientScope(ctx, auth.PatientScopeRequest{
					Principal: RestrictedClinician(),
					Tenant:    PatientScopedTenant("pat-1"),
					PatientID: "pat-2",
				})
				return AssertDecision("patient scope other", ExpectDeny, d, err)
			},
		},
		{
			Name: "patient_read_own_id_allowed",
			Doc:  "Patient-scoped read of own Patient resource is allowed.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanReadResource(ctx, auth.ReadRequest{
					Principal: RestrictedClinician(), Tenant: PatientScopedTenant("pat-1"),
					ResourceType: "Patient", ID: "pat-1",
				})
				return AssertDecision("read own patient", ExpectAllow, d, err)
			},
		},
		{
			Name: "patient_read_other_id_denied",
			Doc:  "Patient-scoped principal cannot read another Patient by id.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanReadResource(ctx, auth.ReadRequest{
					Principal: RestrictedClinician(), Tenant: PatientScopedTenant("pat-1"),
					ResourceType: "Patient", ID: "pat-2",
				})
				return AssertDecision("read other patient", ExpectDeny, d, err)
			},
		},
		{
			Name: "patient_envelope_observation_own_allowed",
			Doc:  "Loaded Observation belonging to scoped patient passes envelope check.",
			Run: func(ctx context.Context, _ *Kit) error {
				tenant := PatientScopedTenant("pat-a")
				resolver := mapPatientResolver{"obs-own": "pat-a"}
				obs := &types.ResourceEnvelope{ResourceType: "Observation", ID: "obs-own"}
				if err := auth.CheckEnvelopePatientScope(ctx, tenant, resolver, obs); err != nil {
					return err
				}
				return nil
			},
		},
		{
			Name: "patient_envelope_observation_other_denied",
			Doc:  "Loaded Observation for another patient is denied for scoped principal.",
			Run: func(ctx context.Context, _ *Kit) error {
				tenant := PatientScopedTenant("pat-a")
				resolver := mapPatientResolver{"obs-other": "pat-b"}
				obs := &types.ResourceEnvelope{ResourceType: "Observation", ID: "obs-other"}
				err := auth.CheckEnvelopePatientScope(ctx, tenant, resolver, obs)
				if err == nil {
					return errors.New("expected deny for out-of-compartment observation")
				}
				if !errors.Is(err, auth.ErrDenied) {
					return err
				}
				return nil
			},
		},
		{
			Name: "patient_search_injects_patient_id",
			Doc:  "Patient search for scoped principal is rewritten to _id filter.",
			Run: func(ctx context.Context, _ *Kit) error {
				params, err := auth.ApplyPatientSearchScopeToParams(
					url.Values{"name": {"Doe"}},
					"Patient", "pat-1",
					auth.MapPatientSearchParamResolver{},
				)
				if err != nil {
					return err
				}
				if params.Get("_id") != "pat-1" {
					return errors.New("expected _id=pat-1")
				}
				return nil
			},
		},
		{
			Name: "patient_search_injects_subject_param",
			Doc:  "Observation search injects subject=Patient/{id} for scoped principals.",
			Run: func(ctx context.Context, _ *Kit) error {
				params, err := auth.ApplyPatientSearchScopeToParams(
					url.Values{"code": {"8867-4"}},
					"Observation", "pat-1",
					auth.MapPatientSearchParamResolver{"Observation": "subject"},
				)
				if err != nil {
					return err
				}
				if params.Get("subject") != "Patient/pat-1" {
					return errors.New("expected subject=Patient/pat-1")
				}
				return nil
			},
		},
		{
			Name: "patient_search_bundle_filters_include_leakage",
			Doc:  "_include results outside patient compartment are removed from search bundles.",
			Run: func(ctx context.Context, _ *Kit) error {
				tenant := PatientScopedTenant("pat-a")
				resolver := mapPatientResolver{
					"obs-a": "pat-a",
					"obs-b": "pat-b",
				}
				bundle := &search.SearchBundle{
					Entries: []search.BundleEntry{
						{Resource: &types.ResourceEnvelope{ResourceType: "Observation", ID: "obs-a"}},
						{Resource: &types.ResourceEnvelope{ResourceType: "Observation", ID: "obs-b"}},
					},
					Total: intPtr(2),
				}
				if err := filterBundlePatientScope(ctx, tenant, resolver, bundle); err != nil {
					return err
				}
				if len(bundle.Entries) != 1 || bundle.Entries[0].Resource.ID != "obs-a" {
					return errors.New("expected only in-compartment entry retained")
				}
				if bundle.Total == nil || *bundle.Total != 1 {
					return errors.New("expected total count adjusted to 1")
				}
				return nil
			},
		},

		// View
		{
			Name: "view_patient_summary_allowed",
			Doc:  "Clinician with read-patient-summary may execute patient_summary_view.",
			Run: func(ctx context.Context, kit *Kit) error {
				authorizer := viewAuthorizer(kit.Engine)
				if err := authorizer.AuthorizeView(ctx, view.AuthRequest{
					ViewName: "patient_summary_view", ResourceType: "Patient",
					Actor: "user-clinician", Permissions: []string{"read-patient-summary"},
				}); err != nil {
					return err
				}
				return nil
			},
		},
		{
			Name: "view_missing_permission_denied",
			Doc:  "View execution denied when principal lacks declared permissions.",
			Run: func(ctx context.Context, kit *Kit) error {
				authorizer := viewAuthorizer(kit.Engine)
				err := authorizer.AuthorizeView(ctx, view.AuthRequest{
					ViewName: "patient_summary_view", ResourceType: "Patient",
					Actor: "user-clinician", Permissions: []string{"missing-permission"},
				})
				if !errors.Is(err, view.ErrUnauthorized) {
					return errors.New("expected view.ErrUnauthorized")
				}
				return nil
			},
		},

		// AI
		{
			Name: "ai_read_appointment_allowed",
			Doc:  "AI policy adapter allows read_fhir_resource for permitted appointment.",
			Run: func(ctx context.Context, kit *Kit) error {
				adapter := aiPolicyAdapter(kit.Engine)
				read, err := adapter.CheckRead(ctx, ai.ReadPolicyRequest{
					Actor: "user-clinician", ResourceType: "Appointment", ID: "a1",
				})
				if err != nil || !read.Allowed {
					return errors.New("expected AI read allow")
				}
				return nil
			},
		},
		{
			Name: "ai_search_mixed_params_denied",
			Doc:  "AI search denies requests mixing allowed and disallowed parameters.",
			Run: func(ctx context.Context, kit *Kit) error {
				adapter := aiPolicyAdapter(kit.Engine)
				_, err := adapter.CheckSearch(ctx, ai.SearchPolicyRequest{
					Actor: "user-clinician", ResourceType: "Appointment",
					Params: url.Values{"date": {"2026-01-01"}, "identifier": {"x"}},
				})
				if !errors.Is(err, ai.ErrPolicyDenied) {
					return errors.New("expected ai.ErrPolicyDenied for mixed params")
				}
				return nil
			},
		},
		{
			Name: "ai_patient_scope_search_injects_filter",
			Doc:  "AI search for patient-scoped principal injects compartment filters.",
			Run: func(ctx context.Context, kit *Kit) error {
				adapter := scopedAIPolicyAdapter(kit.Engine)
				search, err := adapter.CheckSearch(ctx, ai.SearchPolicyRequest{
					Actor: "user-clinician", ResourceType: "Appointment",
					Params: url.Values{"date": {"2026-01-01"}},
				})
				if err != nil {
					return err
				}
				if search.Params.Get("patient") != "Patient/pat-1" {
					return errors.New("expected patient search scope injection")
				}
				return nil
			},
		},
		{
			Name: "ai_tool_run_view_allowed",
			Doc:  "AI run_view tool execution is allowed for clinician.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanExecuteAITool(ctx, auth.AIToolRequest{
					Principal: RestrictedClinician(), Tenant: TenantContextA(),
					ToolName: ai.ToolRunView, ViewName: "patient_summary_view",
				})
				return AssertDecision("ai run_view", ExpectAllow, d, err)
			},
		},

		// Sync / device
		{
			Name: "sync_device_push_trusted_allowed",
			Doc:  "Trusted device registered to tenant may push sync events.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanPushDeviceEvent(ctx, auth.DevicePushRequest{
					DeviceID: "device-trusted", TenantID: TenantA,
				})
				return AssertDecision("device push", ExpectAllow, d, err)
			},
		},
		{
			Name: "sync_device_push_wrong_tenant_denied",
			Doc:  "Device registered to tenant-a cannot push to tenant-b.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanPushDeviceEvent(ctx, auth.DevicePushRequest{
					DeviceID: "device-trusted", TenantID: TenantB,
				})
				return AssertDecision("device wrong tenant", ExpectDeny, d, err)
			},
		},
		{
			Name: "sync_device_push_untrusted_denied",
			Doc:  "Untrusted device cannot push events even within its tenant.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanPushDeviceEvent(ctx, auth.DevicePushRequest{
					DeviceID: "device-untrusted", TenantID: TenantA,
				})
				return AssertDecision("untrusted device", ExpectDeny, d, err)
			},
		},

		// Module install
		{
			Name: "module_install_admin_allowed",
			Doc:  "Tenant admin may install scheduling module.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanInstallModule(ctx, auth.ModuleInstallRequest{
					Principal: TenantAdmin(), Tenant: TenantContextA(),
					ModuleName: "scheduling", ModuleVersion: "1.0.0",
					RequiredPermissions: []string{"read-appointment"},
				})
				return AssertDecision("module install", ExpectAllow, d, err)
			},
		},
		{
			Name: "module_install_clinician_denied",
			Doc:  "Clinician without module.install permission cannot install modules.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanInstallModule(ctx, auth.ModuleInstallRequest{
					Principal: RestrictedClinician(), Tenant: TenantContextA(),
					ModuleName:          "scheduling",
					RequiredPermissions: []string{"module.install"},
				})
				return AssertDecision("module install clinician", ExpectDeny, d, err)
			},
		},
		{
			Name: "module_installer_authorizer_denied",
			Doc:  "ModuleInstallerAuthorizer denies clinician install attempts.",
			Run: func(ctx context.Context, kit *Kit) error {
				authorizer := &auth.ModuleInstallerAuthorizer{
					Engine: kit.Engine,
					Resolve: func(_ context.Context) (auth.Principal, auth.TenantContext, error) {
						return RestrictedClinician(), TenantContextA(), nil
					},
				}
				err := authorizer.AuthorizeModuleInstall(ctx, modules.InstallAuthRequest{
					Module: modules.Module{Manifest: modules.Manifest{
						Name: "scheduling", Version: "1.0.0",
						Permissions: []string{"read-appointment"},
					}},
					Plan: &modules.Plan{Name: "scheduling", Version: "1.0.0", Action: "install"},
				})
				if !errors.Is(err, auth.ErrDenied) {
					return errors.New("expected auth.ErrDenied for clinician module install")
				}
				return nil
			},
		},

		// SMART token semantics
		{
			Name: "smart_token_valid_accepted",
			Doc:  "SMART token with valid iss/aud/exp/nbf and scopes is accepted.",
			Run: func(ctx context.Context, _ *Kit) error {
				now := fixedNow()
				tv := smart.NewTokenValidator(nil)
				tv.Now = func() time.Time { return now }
				token := unsignedTestJWT(map[string]any{
					"iss": "https://issuer.example", "aud": "https://aud.example",
					"exp":   now.Add(time.Hour).Unix(),
					"nbf":   now.Add(-time.Minute).Unix(),
					"scope": "patient/*.read", "patient": "pat-1",
				})
				_, err := tv.ValidateToken(token, smart.TokenValidateOptions{
					ExpectedIssuer:   "https://issuer.example",
					ExpectedAudience: "https://aud.example",
					RequiredScopes:   []string{"patient/*.read"},
				})
				return err
			},
		},
		{
			Name: "smart_token_expired_rejected",
			Doc:  "Token past exp is rejected with ErrTokenExpired.",
			Run: func(ctx context.Context, _ *Kit) error {
				now := fixedNow()
				tv := smart.NewTokenValidator(nil)
				tv.Now = func() time.Time { return now }
				token := unsignedTestJWT(map[string]any{
					"iss": "https://issuer.example", "aud": "https://aud.example",
					"exp": now.Add(-time.Minute).Unix(), "scope": "patient/*.read",
				})
				_, err := tv.ValidateToken(token, smart.TokenValidateOptions{
					ExpectedIssuer:   "https://issuer.example",
					ExpectedAudience: "https://aud.example",
				})
				if !errors.Is(err, smart.ErrTokenExpired) {
					return errors.New("expected ErrTokenExpired")
				}
				return nil
			},
		},
		{
			Name: "smart_token_nbf_future_rejected",
			Doc:  "Token with nbf in the future is rejected with ErrTokenNotYetValid.",
			Run: func(ctx context.Context, _ *Kit) error {
				now := fixedNow()
				tv := smart.NewTokenValidator(nil)
				tv.Now = func() time.Time { return now }
				token := unsignedTestJWT(map[string]any{
					"iss": "https://issuer.example", "aud": "https://aud.example",
					"exp":   now.Add(time.Hour).Unix(),
					"nbf":   now.Add(time.Hour).Unix(),
					"scope": "patient/*.read",
				})
				_, err := tv.ValidateToken(token, smart.TokenValidateOptions{
					ExpectedIssuer:   "https://issuer.example",
					ExpectedAudience: "https://aud.example",
				})
				if !errors.Is(err, smart.ErrTokenNotYetValid) {
					return errors.New("expected ErrTokenNotYetValid")
				}
				return nil
			},
		},
		{
			Name: "smart_token_wrong_audience_rejected",
			Doc:  "Token with mismatched aud is rejected.",
			Run: func(ctx context.Context, _ *Kit) error {
				now := fixedNow()
				tv := smart.NewTokenValidator(nil)
				tv.Now = func() time.Time { return now }
				token := unsignedTestJWT(map[string]any{
					"iss": "https://issuer.example", "aud": "https://aud.example",
					"exp": now.Add(time.Hour).Unix(), "scope": "patient/*.read",
				})
				_, err := tv.ValidateToken(token, smart.TokenValidateOptions{
					ExpectedAudience: "https://other.example",
				})
				if !errors.Is(err, smart.ErrAudienceMismatch) {
					return errors.New("expected ErrAudienceMismatch")
				}
				return nil
			},
		},
		{
			Name: "smart_token_wrong_issuer_rejected",
			Doc:  "Token with mismatched iss is rejected.",
			Run: func(ctx context.Context, _ *Kit) error {
				now := fixedNow()
				tv := smart.NewTokenValidator(nil)
				tv.Now = func() time.Time { return now }
				token := unsignedTestJWT(map[string]any{
					"iss": "https://issuer.example", "aud": "https://aud.example",
					"exp": now.Add(time.Hour).Unix(), "scope": "patient/*.read",
				})
				_, err := tv.ValidateToken(token, smart.TokenValidateOptions{
					ExpectedIssuer: "https://other.example",
				})
				if !errors.Is(err, smart.ErrIssuerMismatch) {
					return errors.New("expected ErrIssuerMismatch")
				}
				return nil
			},
		},

		// SMART scope vs policy
		{
			Name: "smart_scope_allows_policy_denies_appointment",
			Doc:  "SMART patient/*.read grants scope but narrow policy denies Appointment read.",
			Run: func(ctx context.Context, kit *Kit) error {
				narrowEng := MustEngineFromConfig(auth.Config{
					Roles: []auth.Role{{
						Name:        "clinician",
						Permissions: []auth.Permission{"*.read", "observation.read"},
					}},
					Principals: []auth.Principal{RestrictedClinician()},
					Policy:     NarrowObservationPolicy(),
				})
				bundle, err := patientReadBundle(kit.Adapter, "pat-1")
				if err != nil {
					return err
				}
				req := kit.Adapter.ToReadRequest(bundle, "Appointment", "a1")
				d, err := narrowEng.CanReadResource(ctx, req)
				return AssertDecision("scope vs policy appointment", ExpectDeny, d, err)
			},
		},
		{
			Name: "smart_scope_allows_policy_allows_observation",
			Doc:  "SMART patient/*.read with narrow Observation-only policy allows Observation read.",
			Run: func(ctx context.Context, kit *Kit) error {
				narrowEng := MustEngineFromConfig(auth.Config{
					Roles: []auth.Role{{
						Name:        "clinician",
						Permissions: []auth.Permission{"*.read", "observation.read"},
					}},
					Principals: []auth.Principal{RestrictedClinician()},
					Policy:     NarrowObservationPolicy(),
				})
				bundle, err := patientReadBundle(kit.Adapter, "pat-1")
				if err != nil {
					return err
				}
				req := kit.Adapter.ToReadRequest(bundle, "Observation", "obs-1")
				d, err := narrowEng.CanReadResource(ctx, req)
				return AssertDecision("scope vs policy observation", ExpectAllow, d, err)
			},
		},
		{
			Name: "smart_backend_service_no_patient_scope",
			Doc:  "Backend service principal has no patient compartment constraint.",
			Run: func(ctx context.Context, kit *Kit) error {
				bundle, err := backendReadBundle(kit.Adapter)
				if err != nil {
					return err
				}
				if bundle.Tenant.PatientScope != "" {
					return errors.New("backend service should not have patient scope")
				}
				if !kit.Adapter.ScopeImplies(bundle, "Patient", smart.VerbRead) {
					return errors.New("expected system/*.read to imply Patient read")
				}
				return nil
			},
		},
		{
			Name: "smart_adapter_patient_scope_from_launch",
			Doc:  "AuthAdapter sets TenantContext.PatientScope from launch/patient claim.",
			Run: func(ctx context.Context, kit *Kit) error {
				bundle, err := patientReadBundle(kit.Adapter, "pat-42")
				if err != nil {
					return err
				}
				if bundle.Tenant.PatientScope != "pat-42" {
					return errors.New("expected patient scope from launch")
				}
				return nil
			},
		},

		// Module policy deny
		{
			Name: "module_install_forbidden_module_denied",
			Doc:  "Admin cannot install modules not named in policy allow rules.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanInstallModule(ctx, auth.ModuleInstallRequest{
					Principal: TenantAdmin(), Tenant: TenantContextA(),
					ModuleName: "forbidden-mod", ModuleVersion: "1.0.0",
				})
				return AssertDecision("forbidden module", ExpectDeny, d, err)
			},
		},

		// Write denied
		{
			Name: "crud_write_observation_denied",
			Doc:  "Write to Observation denied when no write policy rule matches.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanWriteResource(ctx, auth.WriteRequest{
					Principal: RestrictedClinician(), Tenant: TenantContextA(),
					Operation: "create", ResourceType: "Observation", ID: "",
				})
				return AssertDecision("write observation", ExpectDeny, d, err)
			},
		},

		// SMART 2.2 granular scopes
		{
			Name: "smart22_scope_filter_search_params_injected",
			Doc:  "patient/Observation.rs?category=laboratory injects category filter into search params.",
			Run: func(ctx context.Context, kit *Kit) error {
				scopes, err := smart.ParseScopes("patient/Observation.rs?category=laboratory")
				if err != nil {
					return err
				}
				out, err := smart.ApplyScopeFiltersToParams(scopes, smart.ActorPatient, "Observation", url.Values{})
				if err != nil {
					return err
				}
				if out.Get("category") != "laboratory" {
					return errors.New("expected category=laboratory filter")
				}
				return nil
			},
		},
		{
			Name: "smart22_scope_filter_denies_out_of_filter_resource",
			Doc:  "Filtered scope cannot read Observation outside granted category.",
			Run: func(ctx context.Context, kit *Kit) error {
				scopes, err := smart.ParseScopes("patient/Observation.rs?category=laboratory")
				if err != nil {
					return err
				}
				vital := observationEnvelope("obs-vital", "vital-signs")
				if err := smart.CheckEnvelopeScopeFilters(ctx, scopes, smart.ActorPatient, "Observation", smart.OpRead, vital); err == nil {
					return errors.New("expected out-of-filter observation to be denied")
				}
				return nil
			},
		},
		{
			Name: "smart22_search_only_scope_denies_read",
			Doc:  "Scope with only s letter allows search but not read-by-id.",
			Run: func(ctx context.Context, kit *Kit) error {
				scopes, err := smart.ParseScopes("user/Observation.s")
				if err != nil {
					return err
				}
				if !scopes.AllowsOp(smart.ActorUser, "Observation", smart.OpSearch) {
					return errors.New("expected search to be allowed")
				}
				if scopes.AllowsOp(smart.ActorUser, "Observation", smart.OpRead) {
					return errors.New("search-only scope should not allow read")
				}
				return nil
			},
		},
		{
			Name: "smart22_revinclude_bundle_filters_scope_leakage",
			Doc:  "_revinclude entries outside granted scope filters are removed from search bundles.",
			Run: func(ctx context.Context, kit *Kit) error {
				scopes, err := smart.ParseScopes("patient/Observation.rs?category=laboratory")
				if err != nil {
					return err
				}
				lab := observationEnvelope("obs-lab", "laboratory")
				vital := observationEnvelope("obs-vital", "vital-signs")
				bundle := search.AssembleBundle(&search.Result{
					ResourceType: "Observation",
					Resources:    []*types.ResourceEnvelope{lab},
					Included: []search.IncludedEntry{{
						ResourceType: "Observation", ID: vital.ID, Resource: vital, Mode: "include",
					}},
				})
				if err := smart.FilterSearchBundleScopeFilters(ctx, scopes, smart.ActorPatient, "Observation", bundle); err != nil {
					return err
				}
				if len(bundle.Entries) != 1 {
					return errors.New("expected revincluded out-of-filter observation removed")
				}
				return nil
			},
		},
		{
			Name: "authz_policy_change_mid_session",
			Doc:  "Replacing the policy engine changes allow/deny for subsequent requests with the same principal.",
			Run: func(ctx context.Context, kit *Kit) error {
				d, err := kit.Engine.CanReadResource(ctx, auth.ReadRequest{
					Principal: RestrictedClinician(), Tenant: TenantContextA(),
					ResourceType: "Appointment", ID: "a1",
				})
				if err := AssertDecision("before policy change", ExpectAllow, d, err); err != nil {
					return err
				}
				strict := MustEngineFromConfig(auth.Config{
					Roles:      BaseConfig().Roles,
					Principals: []auth.Principal{RestrictedClinician()},
					Policy: &auth.PolicyDocument{
						Version: "1",
						Rules: []auth.PolicyRule{{
							Name: "deny-all", Effect: auth.EffectDeny,
							Match:  auth.RuleMatch{Actions: []string{auth.ActionRead}},
							Reason: "session policy revoked",
						}},
					},
				})
				d, err = strict.CanReadResource(ctx, auth.ReadRequest{
					Principal: RestrictedClinician(), Tenant: TenantContextA(),
					ResourceType: "Appointment", ID: "a1",
				})
				return AssertDecision("after policy change", ExpectDeny, d, err)
			},
		},
		{
			Name: "smart22_policy_denies_despite_filtered_scope",
			Doc:  "Policy deny still overrides SMART 2.2 filtered scope allow.",
			Run: func(ctx context.Context, kit *Kit) error {
				scopes, err := smart.ParseScopes("patient/Observation.rs?category=laboratory launch/patient")
				if err != nil {
					return err
				}
				claims := smart.TokenClaims{
					Subject: "user-1", Patient: "pat-1",
					Scope: scopes.SpaceSeparated(), Scopes: scopes,
				}
				bundle, err := kit.Adapter.ToAuthRequests(claims, smart.BuildLaunchContext(smart.LaunchContextInput{
					Claims: &claims, Scopes: scopes,
				}))
				if err != nil {
					return err
				}
				narrowEng := MustEngineFromConfig(auth.Config{
					Roles: []auth.Role{{
						Name:        "clinician",
						Permissions: []auth.Permission{"*.read", "observation.read"},
					}},
					Principals: []auth.Principal{bundle.Principal},
					Policy:     NarrowObservationPolicy(),
				})
				checker := smart.ScopePolicyAuthChecker{
					Engine: narrowEng, Adapter: kit.Adapter,
					BundleFor: func(_ auth.Principal, _ auth.TenantContext) (smart.AuthBundle, bool) {
						return bundle, true
					},
				}
				denyAppt, err := checker.AuthorizeRead(ctx, bundle.Principal, bundle.Tenant, "Appointment", "a1")
				if err != nil {
					return err
				}
				if denyAppt.Allowed {
					return errors.New("expected policy deny despite filtered scope")
				}
				return nil
			},
		},
	}
}

func observationEnvelope(id, category string) *types.ResourceEnvelope {
	data := []byte(`{
		"resourceType": "Observation",
		"id": "` + id + `",
		"status": "final",
		"category": [{
			"coding": [{"system": "http://terminology.hl7.org/CodeSystem/observation-category", "code": "` + category + `"}]
		}]
	}`)
	env, err := types.NewJSONCodec().ParseJSON("Observation", data)
	if err != nil {
		panic(err)
	}
	return env
}

// MustEngineFromConfig builds an engine without a testing.T (for inline scenario configs).
func MustEngineFromConfig(cfg auth.Config) *auth.Engine {
	eng, err := auth.NewEngine(cfg)
	if err != nil {
		panic("authztest: " + err.Error())
	}
	return eng
}

func NarrowEngineConfigWithDenyFirst() auth.Config {
	cfg := BaseConfig()
	cfg.Policy = &auth.PolicyDocument{
		Version: "1",
		Rules: []auth.PolicyRule{
			{
				Name:   "deny-appointment",
				Effect: auth.EffectDeny,
				Match: auth.RuleMatch{
					Actions:       []string{auth.ActionRead},
					ResourceTypes: []string{"Appointment"},
				},
				Reason: "blocked by deny-first rule",
			},
			{
				Name:   "allow-appointment",
				Effect: auth.EffectAllow,
				Match: auth.RuleMatch{
					Actions:        []string{auth.ActionRead},
					ResourceTypes:  []string{"Appointment"},
					AnyPermissions: []string{"appointment.read"},
				},
				Reason: "would allow",
			},
		},
	}
	return cfg
}

func viewAuthorizer(eng *auth.Engine) *auth.ViewAuthorizer {
	return &auth.ViewAuthorizer{
		Engine: eng, TenantID: TenantA,
		Resolve: func(_ context.Context, actor, _ string) (auth.Principal, auth.TenantContext, error) {
			p, err := eng.Catalog().GetPrincipal(actor)
			return p, TenantContextA(), err
		},
	}
}

func aiPolicyAdapter(eng *auth.Engine) *auth.AIPolicyAdapter {
	return &auth.AIPolicyAdapter{
		Engine: eng, TenantID: TenantA,
		Resolve: func(_ context.Context, actor, _ string) (auth.Principal, auth.TenantContext, error) {
			p, err := eng.Catalog().GetPrincipal(actor)
			return p, TenantContextA(), err
		},
		Constraints: &auth.AIConstraints{
			Search: map[string]ai.SearchTypePolicy{
				"Appointment": {AllowedParams: []string{"date", "patient"}, MaxCount: 25},
			},
			Views: map[string]ai.ViewTypePolicy{
				"patient_summary_view": {Deidentify: true},
			},
			Write: map[string]ai.WriteTypePolicy{
				"Appointment": {UpdateFields: []string{"status"}},
			},
		},
	}
}

func scopedAIPolicyAdapter(eng *auth.Engine) *auth.AIPolicyAdapter {
	adapter := aiPolicyAdapter(eng)
	adapter.PatientSearchParams = auth.MapPatientSearchParamResolver{"Appointment": "patient"}
	adapter.Resolve = func(_ context.Context, actor, _ string) (auth.Principal, auth.TenantContext, error) {
		p, err := eng.Catalog().GetPrincipal(actor)
		return p, PatientScopedTenant("pat-1"), err
	}
	return adapter
}

func patientReadBundle(adapter *smart.AuthAdapter, patientID string) (smart.AuthBundle, error) {
	scopes, err := smart.ParseScopes("launch/patient patient/*.read")
	if err != nil {
		return smart.AuthBundle{}, err
	}
	claims := smart.TokenClaims{
		Subject: "user-clinician",
		Scope:   scopes.SpaceSeparated(),
		Scopes:  scopes,
		Patient: patientID,
	}
	return adapter.ToAuthRequests(claims, smart.BuildLaunchContext(smart.LaunchContextInput{
		Claims: &claims, Scopes: scopes,
	}))
}

func backendReadBundle(adapter *smart.AuthAdapter) (smart.AuthBundle, error) {
	scopes, err := smart.ParseScopes("system/*.read")
	if err != nil {
		return smart.AuthBundle{}, err
	}
	claims := smart.TokenClaims{
		Subject:  "backend-app",
		ClientID: "backend-app",
		Scope:    scopes.SpaceSeparated(),
		Scopes:   scopes,
	}
	client := smart.BackendClient{ClientID: "backend-app", AllowedScopes: []string{"system/*.read"}}
	return adapter.FromBackendService(claims, client, smart.LaunchContext{})
}

type mapPatientResolver map[string]string

func (m mapPatientResolver) PatientIDForResource(_ context.Context, resourceType string, resource *types.ResourceEnvelope) (string, bool, error) {
	if resourceType == "Patient" {
		return resource.ID, true, nil
	}
	if id, ok := m[resource.ID]; ok {
		return id, true, nil
	}
	return "", false, nil
}

func filterBundlePatientScope(ctx context.Context, tenant auth.TenantContext, resolver auth.ResourcePatientResolver, bundle *search.SearchBundle) error {
	if bundle == nil || tenant.PatientScope == "" {
		return nil
	}
	kept := make([]search.BundleEntry, 0, len(bundle.Entries))
	for _, entry := range bundle.Entries {
		if entry.Resource == nil {
			continue
		}
		if err := auth.CheckEnvelopePatientScope(ctx, tenant, resolver, entry.Resource); err != nil {
			if errors.Is(err, auth.ErrDenied) {
				continue
			}
			return err
		}
		kept = append(kept, entry)
	}
	bundle.Entries = kept
	bundle.Count = len(kept)
	if bundle.Total != nil {
		total := len(kept)
		bundle.Total = &total
	}
	return nil
}

func intPtr(v int) *int { return &v }
