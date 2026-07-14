// Package smart implements haistack-smart, an optional SMART on FHIR library for
// Health AI Stack.
//
// # Scope
//
// v1 establishes SMART as a separate interpreter that plugs into haistack-auth
// through adapters. It does not move SMART concepts into pkg/auth, and it is not
// required by offline/local runtimes (core, sync, view, ai, auth).
//
// Contained in v1:
//
//   - SMART scope parsing and normalized ScopeSet matching
//   - LaunchContext types and builders (no EHR/standalone launch runtime)
//   - Token / JWT claim validation helpers
//   - Backend-service assertion validation and client registry
//   - AuthAdapter translating scopes + launch context into pkg/auth inputs
//   - Minimal ClientRegistration types for future expansion
//
// Explicitly out of v1: full SMART App Launch product flow, EHR launch
// orchestration, standalone launch orchestration, dynamic client registration,
// refresh-token lifecycle, SMART UI/session management, and HTTP middleware as
// the center of the package.
//
// # Public API
//
//   - ParseScopes / ScopeParser: normalize patient|user|system resource scopes
//     and launch/specialty tokens
//   - ScopeSet.Allows / AllowsRead / AllowsWrite: resource/action matching
//   - BuildLaunchContext / ExtractLaunchContext: patient/encounter/user context
//   - TokenValidator / ValidateToken / ValidateClaims: JWT structure, iss/aud/exp/nbf,
//     scope extraction (optional SignatureVerifier / PEMVerifier)
//   - BackendServiceAuth / ValidateBackendAssertion: signed client assertions,
//     allow-listed scopes, service-client registry
//   - AuthAdapter / ToAuthRequests / FromBackendService: derive auth.Principal,
//     TenantContext (including PatientScope), and request builders for pkg/auth
//   - ClientRegistration: minimal static client metadata
//
// # Typical usage
//
//	scopes, err := smart.ParseScopes("patient/*.read launch/patient")
//	ok := scopes.AllowsRead(smart.ActorPatient, "Observation")
//
//	launch := smart.BuildLaunchContext(smart.LaunchContextInput{
//	    PatientID:  "pat-1",
//	    UserID:     "Practitioner/pract-1",
//	    TenantHint: "tenant-a",
//	    Scopes:     scopes,
//	})
//
//	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{
//	    DefaultTenantID:  "tenant-a",
//	    DefaultUserRoles: []string{"smart-user"},
//	})
//	bundle, err := adapter.ToAuthRequests(claims, launch)
//	_ = eng.CheckPatientScope(ctx, adapter.ToPatientScopeRequest(bundle, ""))
//
//	bsa, _ := smart.NewBackendServiceAuth("https://auth.example/token", client)
//	claims, client, err := bsa.ValidateBackendAssertion(assertionJWT, smart.TokenValidateOptions{})
//	bundle, err = adapter.FromBackendService(claims, client, smart.LaunchContext{})
//
// # Integration
//
//   - haistack-auth: AuthAdapter produces Principal, TenantContext, permissions,
//     ReadRequest/WriteRequest/PatientScopeRequest; Engine remains the decision
//     authority
//   - Hosts own OAuth/token-endpoint transport; this package validates and adapts
package smart
