package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/degoke/health-ai-stack/examples/internal/appkit"
	"github.com/degoke/health-ai-stack/pkg/auth"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "smart-authz: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "haistack-smart-authz-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	stack, err := appkit.NewSQLiteStack(ctx, filepath.Join(tempDir, "smart.db"), "Patient", "Observation")
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	patientA, err := appkit.EnvelopeFromJSON("Patient", appkit.PatientJSON("Alice", "Scoped", "+1-555-0101"))
	if err != nil {
		return err
	}
	patientB, err := appkit.EnvelopeFromJSON("Patient", appkit.PatientJSON("Bob", "Other", "+1-555-0102"))
	if err != nil {
		return err
	}
	createdA, err := stack.ResourceService.Create(ctx, patientA)
	if err != nil {
		return err
	}
	createdB, err := stack.ResourceService.Create(ctx, patientB)
	if err != nil {
		return err
	}

	authEngine, adapter, bundles, err := buildAuthStack(createdA.ID)
	if err != nil {
		return err
	}
	scopeMatcher := smart.RegistryScopeFilterMatcherChain(stack.SearchRegistry, stack.FHIRPath)

	searchAdapter := hahttp.SearchServiceAdapter{
		Svc:                        stack.SearchService,
		PatientSearchParamResolver: stack.Snapshot,
	}

	patientRefResolver := &registry.PatientReferenceResolver{
		Snapshot: stack.Snapshot,
		Engine:   stack.FHIRPath,
	}

	handler, err := hahttp.NewHandler(hahttp.Config{
		ResourceService:          hahttp.CoreResourceService{Svc: stack.ResourceService},
		SearchService:            searchAdapter,
		CapabilitySource:         hahttp.RegistryCapabilitySource{Snapshot: stack.Snapshot},
		PatientReferenceResolver: patientRefResolver,
		ScopeFilterMatcher:       scopeMatcher,
		PrincipalResolver:        principalResolver(bundles),
		AuthBundleResolver:       authBundleResolver(bundles),
		AuthChecker: smart.ScopePolicyAuthChecker{
			Engine:  authEngine,
			Adapter: adapter,
			BundleFor: func(p auth.Principal, t auth.TenantContext) (smart.AuthBundle, bool) {
				if b, ok := bundles[p.ID]; ok {
					return b, true
				}
				return smart.AuthBundle{}, false
			},
		},
	})
	if err != nil {
		return err
	}

	fmt.Println("SMART authorization demo (token success ≠ authorization)")
	fmt.Println(smart.SMARTVersion)
	fmt.Println()

	// Unrestricted clinician token: may read any patient in policy.
	if err := demoRead(handler, "unrestricted", createdB.ID, http.StatusOK); err != nil {
		return err
	}
	fmt.Printf("unrestricted clinician read Patient/%s: allowed\n", createdB.ID)

	// Patient-scoped launch token: may read own patient only.
	if err := demoRead(handler, "scoped", createdA.ID, http.StatusOK); err != nil {
		return err
	}
	fmt.Printf("patient-scoped read own Patient/%s: allowed\n", createdA.ID)

	if err := demoRead(handler, "scoped", createdB.ID, http.StatusForbidden); err != nil {
		return err
	}
	fmt.Printf("patient-scoped read other Patient/%s: denied (403)\n", createdB.ID)

	// Scope grants patient/*.read but policy allows Observation only for scoped principals.
	if err := demoRead(handler, "narrow", createdA.ID, http.StatusForbidden); err != nil {
		return err
	}
	fmt.Printf("narrow-policy read Patient/%s: denied despite patient/*.read scope\n", createdA.ID)

	cfg := smart.DefaultConfiguration("https://fhir.example")
	fmt.Printf("\nSMART metadata issuer: %s (scopes: %d)\n", cfg.Issuer, len(cfg.ScopesSupported))
	return nil
}

func buildAuthStack(scopedPatientID string) (*auth.Engine, *smart.AuthAdapter, map[string]smart.AuthBundle, error) {
	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{
		DefaultTenantID:  "tenant-demo",
		DefaultUserRoles: []string{"clinician"},
	})
	unrestrictedScopes, _ := smart.ParseScopes("user/Patient.read")
	unrestrictedClaims := smart.TokenClaims{Subject: "clin-unrestricted", Scope: unrestrictedScopes.SpaceSeparated(), Scopes: unrestrictedScopes}
	unrestrictedBundle, err := adapter.ToAuthRequests(unrestrictedClaims, smart.LaunchContext{})
	if err != nil {
		return nil, nil, nil, err
	}

	scopedScopes, _ := smart.ParseScopes("launch/patient patient/*.read")
	scopedClaims := smart.TokenClaims{
		Subject: "cl-scoped", Patient: scopedPatientID,
		Scope: scopedScopes.SpaceSeparated(), Scopes: scopedScopes,
	}
	scopedLaunch := smart.BuildLaunchContext(smart.LaunchContextInput{
		Claims: &scopedClaims, Scopes: scopedScopes, PatientID: scopedPatientID,
	})
	scopedBundle, err := adapter.ToAuthRequests(scopedClaims, scopedLaunch)
	if err != nil {
		return nil, nil, nil, err
	}

	narrowScopes, _ := smart.ParseScopes("patient/*.read launch/patient")
	narrowClaims := smart.TokenClaims{Subject: "cl-narrow", Patient: scopedPatientID, Scope: narrowScopes.SpaceSeparated(), Scopes: narrowScopes}
	narrowBundle, err := adapter.ToAuthRequests(narrowClaims, smart.BuildLaunchContext(smart.LaunchContextInput{
		Claims: &narrowClaims, Scopes: narrowScopes, PatientID: scopedPatientID,
	}))
	if err != nil {
		return nil, nil, nil, err
	}
	narrowBundle.Tenant.PatientScope = scopedPatientID
	narrowBundle.Principal.TenantBindings = []auth.TenantBinding{{
		TenantID: "tenant-demo", Roles: []string{"narrow"},
	}}
	narrowBundle.Tenant.RoleBindings = []string{"narrow"}

	eng, err := auth.NewEngine(auth.Config{
		Roles: []auth.Role{
			{
				Name:        "clinician",
				Permissions: []auth.Permission{"patient.read", "*.read"},
			},
			{
				Name:        "narrow",
				Permissions: []auth.Permission{"*.read"},
			},
		},
		Principals: []auth.Principal{
			unrestrictedBundle.Principal,
			scopedBundle.Principal,
			narrowBundle.Principal,
		},
		PolicyBytes: []byte(`{
  "version": "1",
  "rules": [
    {
      "name": "allow-patient-read-unscoped",
      "effect": "allow",
      "match": {
        "actions": ["read"],
        "resourceTypes": ["Patient"],
        "anyPermissions": ["patient.read", "*.read"],
        "patientScoped": false
      },
      "reason": "unscoped clinicians may read patients"
    },
    {
      "name": "scoped-patient-read",
      "effect": "allow",
      "match": {
        "actions": ["read"],
        "resourceTypes": ["Patient"],
        "patientScoped": true,
        "roles": ["clinician"]
      },
      "reason": "patient-scoped clinicians may read their patient"
    },
    {
      "name": "observation-narrow-only",
      "effect": "allow",
      "match": {
        "actions": ["read"],
        "resourceTypes": ["Observation"],
        "anyPermissions": ["*.read"],
        "roles": ["narrow"]
      },
      "reason": "narrow role: observation only (policy narrows SMART scope)"
    },
    {
      "name": "patient-access",
      "effect": "allow",
      "match": {"actions": ["patient-access"]},
      "reason": "patient compartment"
    }
  ]
}`),
		PolicyFormat: auth.PolicyFormatJSON,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	bundles := map[string]smart.AuthBundle{
		"cl-unrestricted": unrestrictedBundle,
		"cl-scoped":       scopedBundle,
		"cl-narrow":       narrowBundle,
	}
	return eng, adapter, bundles, nil
}

func authBundleResolver(bundles map[string]smart.AuthBundle) hahttp.AuthBundleResolver {
	keys := map[string]string{
		"unrestricted": "cl-unrestricted",
		"scoped":       "cl-scoped",
		"narrow":       "cl-narrow",
	}
	return func(_ context.Context, r *http.Request) (smart.AuthBundle, bool) {
		key := r.Header.Get("X-Demo-Principal")
		id := keys[key]
		if id == "" {
			return smart.AuthBundle{}, false
		}
		bundle, ok := bundles[id]
		return bundle, ok
	}
}

func principalResolver(bundles map[string]smart.AuthBundle) hahttp.PrincipalResolver {
	keys := map[string]string{
		"unrestricted": "cl-unrestricted",
		"scoped":       "cl-scoped",
		"narrow":       "cl-narrow",
	}
	return func(ctx context.Context, r *http.Request) (auth.Principal, auth.TenantContext, error) {
		key := r.Header.Get("X-Demo-Principal")
		id := keys[key]
		if id == "" {
			return auth.Principal{}, auth.TenantContext{}, fmt.Errorf("unknown demo principal %q", key)
		}
		bundle := bundles[id]
		return bundle.Principal, bundle.Tenant, nil
	}
}

func demoRead(handler http.Handler, principalKey, patientID string, wantStatus int) error {
	req := httptest.NewRequest(http.MethodGet, "/fhir/Patient/"+patientID, nil)
	req.Header.Set("X-Demo-Principal", principalKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		return fmt.Errorf("%s read %s: status %d, body %s", principalKey, patientID, rec.Code, rec.Body.String())
	}
	return nil
}
