package smart_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

func TestParseScopes_ValidPatterns(t *testing.T) {
	cases := []struct {
		raw      string
		actor    smart.ActorClass
		resource string
		verb     smart.AccessVerb
	}{
		{"patient/*.read", smart.ActorPatient, "*", smart.VerbRead},
		{"patient/Observation.read", smart.ActorPatient, "Observation", smart.VerbRead},
		{"user/*.write", smart.ActorUser, "*", smart.VerbWrite},
		{"user/Patient.read", smart.ActorUser, "Patient", smart.VerbRead},
		{"user/Appointment.write", smart.ActorUser, "Appointment", smart.VerbWrite},
		{"system/*.read", smart.ActorSystem, "*", smart.VerbRead},
		{"system/*.write", smart.ActorSystem, "*", smart.VerbWrite},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			set, err := smart.ParseScopes(tc.raw)
			if err != nil {
				t.Fatalf("ParseScopes: %v", err)
			}
			if set.Len() != 1 {
				t.Fatalf("len = %d, want 1", set.Len())
			}
			sc := set.Scopes()[0]
			if sc.Actor != tc.actor || sc.Resource != tc.resource || sc.Verb != tc.verb {
				t.Fatalf("scope = %#v", sc)
			}
			if !set.Allows(tc.actor, concreteResource(tc.resource), tc.verb) {
				t.Fatalf("Allows failed for %#v", tc)
			}
		})
	}
}

func concreteResource(resource string) string {
	if resource == "*" {
		return "Patient"
	}
	return resource
}

func TestParseScopes_LaunchAndSpecialty(t *testing.T) {
	set, err := smart.ParseScopes("launch launch/patient openid fhirUser patient/*.read")
	if err != nil {
		t.Fatalf("ParseScopes: %v", err)
	}
	if !set.HasLaunch() {
		t.Fatal("expected launch marker")
	}
	foundSpecialty := false
	for _, sc := range set.Scopes() {
		if sc.Kind == smart.ScopeKindSpecialty && sc.Specialty == "openid" {
			foundSpecialty = true
		}
	}
	if !foundSpecialty {
		t.Fatalf("missing openid: %#v", set.Scopes())
	}
}

func TestParseScopes_Malformed(t *testing.T) {
	bad := []string{
		"patient/",
		"patient.read",
		"foo/*.read",
		"patient/*. coad",
		"patient/*.unknown",
		"patient/*.write",
		"patient/*.*",
		"launch/",
		"patient/bad-name.read",
	}
	for _, raw := range bad {
		_, err := smart.ParseScopes(raw)
		if !errors.Is(err, smart.ErrInvalidScope) {
			t.Fatalf("%q: err = %v, want ErrInvalidScope", raw, err)
		}
	}
}

func TestParseScopes_NormalizeDuplicatesAndOverlaps(t *testing.T) {
	set, err := smart.ParseScopes("patient/*.read patient/*.read patient/Observation.read")
	if err != nil {
		t.Fatalf("ParseScopes: %v", err)
	}
	if set.Len() != 1 {
		t.Fatalf("expected collapsed set, got %#v", set.Scopes())
	}
	if set.Scopes()[0].Raw != "patient/*.read" {
		t.Fatalf("raw = %q", set.Scopes()[0].Raw)
	}
}

func TestScopeSet_Matching(t *testing.T) {
	set, err := smart.ParseScopes("user/Patient.read user/*.write system/*.read")
	if err != nil {
		t.Fatalf("ParseScopes: %v", err)
	}
	if !set.AllowsRead(smart.ActorUser, "Patient") {
		t.Fatal("expected user Patient read")
	}
	if set.AllowsWrite(smart.ActorUser, "Patient") {
		// user/*.write should allow Patient write
	} else {
		t.Fatal("expected user Patient write via user/*.write")
	}
	if !set.AllowsRead(smart.ActorSystem, "Observation") {
		t.Fatal("expected system Observation read")
	}
	if set.AllowsWrite(smart.ActorSystem, "Observation") {
		t.Fatal("system write should be denied")
	}
	if set.AllowsRead(smart.ActorPatient, "Patient") {
		t.Fatal("patient actor should be denied")
	}
}

func TestBuildLaunchContext(t *testing.T) {
	scopes, _ := smart.ParseScopes("launch/patient patient/*.read")
	claims := smart.TokenClaims{
		Patient:    "from-claim",
		Encounter:  "enc-1",
		FHIRUser:   "Practitioner/p1",
		TenantHint: "tenant-claim",
	}
	lc := smart.BuildLaunchContext(smart.LaunchContextInput{
		PatientID: "pat-explicit",
		Scopes:    scopes,
		Claims:    &claims,
	})
	if lc.PatientID != "pat-explicit" {
		t.Fatalf("PatientID = %q", lc.PatientID)
	}
	if lc.EncounterID != "enc-1" || lc.UserID != "Practitioner/p1" {
		t.Fatalf("launch = %#v", lc)
	}
	if lc.Metadata["launch/patient"] != "true" {
		t.Fatalf("metadata = %#v", lc.Metadata)
	}
}

func TestAuthAdapter_ToAuthRequestsAndPatientScope(t *testing.T) {
	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{
		DefaultTenantID:  "tenant-a",
		DefaultUserRoles: []string{"smart-user"},
	})
	scopes, err := smart.ParseScopes("patient/*.read launch/patient")
	if err != nil {
		t.Fatalf("ParseScopes: %v", err)
	}
	claims := smart.TokenClaims{
		Subject: "user-1",
		Scope:   scopes.SpaceSeparated(),
		Scopes:  scopes,
		Patient: "pat-1",
	}
	launch := smart.BuildLaunchContext(smart.LaunchContextInput{Claims: &claims, Scopes: scopes})
	bundle, err := adapter.ToAuthRequests(claims, launch)
	if err != nil {
		t.Fatalf("ToAuthRequests: %v", err)
	}
	if bundle.Principal.Kind != auth.KindUser {
		t.Fatalf("kind = %q", bundle.Principal.Kind)
	}
	if bundle.Tenant.PatientScope != "pat-1" {
		t.Fatalf("PatientScope = %q", bundle.Tenant.PatientScope)
	}
	if !adapter.ScopeImplies(bundle, "Observation", smart.VerbRead) {
		t.Fatal("expected patient/*.read to imply Observation read")
	}
	if adapter.ScopeImplies(bundle, "Observation", smart.VerbWrite) {
		t.Fatal("write should not be implied")
	}

	eng, err := auth.NewEngine(auth.Config{
		Roles: []auth.Role{{
			Name:        "smart-user",
			Permissions: []auth.Permission{"*.read"},
		}},
		Principals: []auth.Principal{bundle.Principal},
		PolicyBytes: []byte(`{
			"version": "1",
			"rules": [{
				"name": "patient-access",
				"effect": "allow",
				"match": {"actions": ["patient-access"]}
			}]
		}`),
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	d, err := eng.CheckPatientScope(context.Background(), adapter.ToPatientScopeRequest(bundle, "pat-1"))
	if err != nil {
		t.Fatalf("CheckPatientScope: %v", err)
	}
	if !d.Allowed {
		t.Fatalf("expected allow, got %#v", d)
	}
	d, err = eng.CheckPatientScope(context.Background(), adapter.ToPatientScopeRequest(bundle, "pat-other"))
	if err != nil {
		t.Fatalf("CheckPatientScope other: %v", err)
	}
	if d.Allowed {
		t.Fatalf("expected deny for other patient, got %#v", d)
	}
}

func TestAuthAdapter_ToAuthRequests_ParsesClaimScopesIntoBundleAndLaunch(t *testing.T) {
	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{
		DefaultTenantID:  "tenant-a",
		DefaultUserRoles: []string{"smart-user"},
	})
	claims := smart.TokenClaims{
		Subject: "user-1",
		Scope:   "launch/patient patient/*.read",
		Patient: "pat-1",
	}
	bundle, err := adapter.ToAuthRequests(claims, smart.LaunchContext{})
	if err != nil {
		t.Fatalf("ToAuthRequests: %v", err)
	}
	if bundle.Scopes.Len() != 2 {
		t.Fatalf("scopes = %#v", bundle.Scopes.Scopes())
	}
	if bundle.Claims.Scopes.Len() != 2 {
		t.Fatalf("bundle claims scopes = %#v", bundle.Claims.Scopes.Scopes())
	}
	if bundle.Launch.Metadata["launch/patient"] != "true" {
		t.Fatalf("launch metadata = %#v", bundle.Launch.Metadata)
	}
	if bundle.Tenant.PatientScope != "pat-1" {
		t.Fatalf("patient scope = %q", bundle.Tenant.PatientScope)
	}
}

func TestAuthAdapter_PermissionsFromScopes(t *testing.T) {
	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{})
	scopes, _ := smart.ParseScopes("user/Patient.read user/Appointment.write")
	perms := adapter.PermissionsFromScopes(scopes)
	if len(perms) != 2 {
		t.Fatalf("perms = %#v", perms)
	}
}

func TestTokenValidator_IssuerAudienceExpiryScopes(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	payload := map[string]any{
		"iss":     "https://issuer.example",
		"sub":     "user-1",
		"aud":     "https://aud.example",
		"exp":     now.Add(time.Hour).Unix(),
		"nbf":     now.Add(-time.Minute).Unix(),
		"scope":   "patient/*.read",
		"patient": "pat-1",
	}
	token := unsignedJWT(t, payload)

	tv := smart.NewTokenValidator(nil)
	tv.Now = func() time.Time { return now }

	claims, err := tv.ValidateToken(token, smart.TokenValidateOptions{
		ExpectedIssuer:   "https://issuer.example",
		ExpectedAudience: "https://aud.example",
		RequiredScopes:   []string{"patient/*.read"},
	})
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.Patient != "pat-1" || claims.Scopes.Len() != 1 {
		t.Fatalf("claims = %#v", claims)
	}

	_, err = tv.ValidateToken(token, smart.TokenValidateOptions{ExpectedIssuer: "other"})
	if !errors.Is(err, smart.ErrIssuerMismatch) {
		t.Fatalf("issuer err = %v", err)
	}
	_, err = tv.ValidateToken(token, smart.TokenValidateOptions{ExpectedAudience: "other"})
	if !errors.Is(err, smart.ErrAudienceMismatch) {
		t.Fatalf("audience err = %v", err)
	}

	expired := unsignedJWT(t, map[string]any{
		"iss":   "https://issuer.example",
		"aud":   "https://aud.example",
		"exp":   now.Add(-time.Minute).Unix(),
		"scope": "patient/*.read",
	})
	_, err = tv.ValidateToken(expired, smart.TokenValidateOptions{
		ExpectedIssuer:   "https://issuer.example",
		ExpectedAudience: "https://aud.example",
	})
	if !errors.Is(err, smart.ErrTokenExpired) {
		t.Fatalf("expired err = %v", err)
	}

	_, err = tv.ValidateToken(token, smart.TokenValidateOptions{
		ExpectedIssuer:   "https://issuer.example",
		ExpectedAudience: "https://aud.example",
		RequiredScopes:   []string{"system/*.read"},
	})
	if !errors.Is(err, smart.ErrMissingScopes) {
		t.Fatalf("missing scopes err = %v", err)
	}
}

func TestBackendServiceAuth_RejectsClientWithoutAllowedScopes(t *testing.T) {
	_, err := smart.NewBackendServiceAuth("https://auth.example/token", smart.BackendClient{
		ClientID: "backend-app",
		Key: smart.ClientKeyMetadata{
			Algorithm:    "RS256",
			PublicKeyPEM: "ignored",
		},
	})
	if !errors.Is(err, smart.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestBackendServiceAuth_DoesNotMutateCallerOpts(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	key, pemPub := mustRSAKey(t)
	client := smart.BackendClient{
		ClientID:      "backend-app",
		AllowedScopes: []string{"system/*.read"},
		Key: smart.ClientKeyMetadata{
			Algorithm:    "RS256",
			PublicKeyPEM: pemPub,
		},
	}
	bsa, err := smart.NewBackendServiceAuth("https://auth.example/token", client)
	if err != nil {
		t.Fatalf("NewBackendServiceAuth: %v", err)
	}
	bsa.Now = func() time.Time { return now }

	assertion := signedJWT(t, key, map[string]any{
		"iss":   "backend-app",
		"sub":   "backend-app",
		"aud":   "https://auth.example/token",
		"exp":   now.Add(5 * time.Minute).Unix(),
		"jti":   "assertion-opts",
		"scope": "system/*.read",
	})
	opts := smart.TokenValidateOptions{ClockSkew: 30 * time.Second}
	if _, _, err := bsa.ValidateBackendAssertion(assertion, opts); err != nil {
		t.Fatalf("ValidateBackendAssertion: %v", err)
	}
	if opts.ClockSkew != 30*time.Second {
		t.Fatalf("caller opts mutated: %#v", opts)
	}
}

func TestPersistentBackendAndReplayStores(t *testing.T) {
	dir := t.TempDir()
	clientStore, err := smart.NewFileBackendClientStore(filepath.Join(dir, "clients.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := smart.BackendClient{ClientID: "backend-app", AllowedScopes: []string{"system/*.read"}}
	if err := clientStore.RegisterBackendClient(client); err != nil {
		t.Fatal(err)
	}
	reloadedClientStore, err := smart.NewFileBackendClientStore(filepath.Join(dir, "clients.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := reloadedClientStore.LookupBackendClient(client.ClientID)
	if err != nil || got.ClientID != client.ClientID {
		t.Fatalf("reloaded client = %#v, err = %v", got, err)
	}

	replay, err := smart.NewFileReplayStore(filepath.Join(dir, "replay.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	replay.Now = func() time.Time { return now }
	if err := replay.CheckAndStore("assertion-1", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	reloadedReplay, err := smart.NewFileReplayStore(filepath.Join(dir, "replay.json"))
	if err != nil {
		t.Fatal(err)
	}
	reloadedReplay.Now = func() time.Time { return now }
	if err := reloadedReplay.CheckAndStore("assertion-1", now.Add(time.Hour)); !errors.Is(err, smart.ErrReplay) {
		t.Fatalf("replayed persistent assertion err = %v", err)
	}
}

func TestBackendServiceAuth_RequiresVerificationKey(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	client := smart.BackendClient{
		ClientID:      "backend-app",
		AllowedScopes: []string{"system/*.read"},
	}
	bsa, err := smart.NewBackendServiceAuth("https://auth.example/token", client)
	if err != nil {
		t.Fatalf("NewBackendServiceAuth: %v", err)
	}
	bsa.Now = func() time.Time { return now }
	_, _, err = bsa.ValidateBackendAssertion(unsignedJWT(t, map[string]any{
		"iss": "backend-app",
		"sub": "backend-app",
		"aud": "https://auth.example/token",
		"exp": now.Add(5 * time.Minute).Unix(),
	}), smart.TokenValidateOptions{})
	if !errors.Is(err, smart.ErrMissingKey) {
		t.Fatalf("err = %v, want ErrMissingKey", err)
	}
}

func TestBackendServiceAuth_ValidateAssertionAndServicePrincipal(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	key, pemPub := mustRSAKey(t)

	client := smart.BackendClient{
		ClientID:      "backend-app",
		AllowedScopes: []string{"system/*.read", "system/*.write"},
		Key: smart.ClientKeyMetadata{
			Algorithm:    "RS256",
			PublicKeyPEM: pemPub,
		},
		TenantHint:  "tenant-svc",
		DisplayName: "Backend App",
	}
	bsa, err := smart.NewBackendServiceAuth("https://auth.example/token", client)
	if err != nil {
		t.Fatalf("NewBackendServiceAuth: %v", err)
	}
	bsa.Now = func() time.Time { return now }

	assertion := signedJWT(t, key, map[string]any{
		"iss":   "backend-app",
		"sub":   "backend-app",
		"aud":   "https://auth.example/token",
		"exp":   now.Add(5 * time.Minute).Unix(),
		"jti":   "assertion-1",
		"scope": "system/*.read",
	})

	claims, gotClient, err := bsa.ValidateBackendAssertion(assertion, smart.TokenValidateOptions{})
	if err != nil {
		t.Fatalf("ValidateBackendAssertion: %v", err)
	}
	if gotClient.ClientID != "backend-app" {
		t.Fatalf("client = %#v", gotClient)
	}
	if !claims.Scopes.AllowsRead(smart.ActorSystem, "Patient") {
		t.Fatal("expected system read")
	}
	if _, _, err = bsa.ValidateBackendAssertion(assertion, smart.TokenValidateOptions{}); !errors.Is(err, smart.ErrReplay) {
		t.Fatalf("replayed assertion err = %v, want ErrReplay", err)
	}

	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{
		DefaultServiceRoles: []string{"backend"},
	})
	bundle, err := adapter.FromBackendService(claims, gotClient, smart.LaunchContext{})
	if err != nil {
		t.Fatalf("FromBackendService: %v", err)
	}
	if bundle.Principal.Kind != auth.KindService {
		t.Fatalf("kind = %q", bundle.Principal.Kind)
	}
	if bundle.Principal.ID != "backend-app" {
		t.Fatalf("id = %q", bundle.Principal.ID)
	}
	if bundle.Tenant.TenantID != "tenant-svc" {
		t.Fatalf("tenant = %q", bundle.Tenant.TenantID)
	}
	if !adapter.ScopeImplies(bundle, "Observation", smart.VerbRead) {
		t.Fatal("expected system scope imply read")
	}

	// Scope outside allow-list
	bad := signedJWT(t, key, map[string]any{
		"iss":   "backend-app",
		"sub":   "backend-app",
		"aud":   "https://auth.example/token",
		"exp":   now.Add(5 * time.Minute).Unix(),
		"jti":   "assertion-bad-scope",
		"scope": "system/*.read patient/*.read",
	})
	_, _, err = bsa.ValidateBackendAssertion(bad, smart.TokenValidateOptions{})
	if !errors.Is(err, smart.ErrScopeNotAllowed) {
		t.Fatalf("scope allow err = %v", err)
	}

	// Unknown client
	other := signedJWT(t, key, map[string]any{
		"iss": "unknown",
		"sub": "unknown",
		"aud": "https://auth.example/token",
		"exp": now.Add(5 * time.Minute).Unix(),
	})
	_, _, err = bsa.ValidateBackendAssertion(other, smart.TokenValidateOptions{})
	if !errors.Is(err, smart.ErrClientNotAllowed) {
		t.Fatalf("unknown client err = %v", err)
	}
}

func TestPackageLevelValidateHelpers(t *testing.T) {
	now := time.Now().UTC()
	token := unsignedJWT(t, map[string]any{
		"iss":   "iss-1",
		"aud":   "aud-1",
		"exp":   now.Add(time.Hour).Unix(),
		"scope": "user/*.read",
	})
	claims, err := smart.ValidateToken(token, nil, smart.TokenValidateOptions{
		ExpectedIssuer:   "iss-1",
		ExpectedAudience: "aud-1",
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.Issuer != "iss-1" {
		t.Fatalf("issuer = %q", claims.Issuer)
	}
}

func TestSubsetOf_BroaderAllowed(t *testing.T) {
	granted, _ := smart.ParseScopes("system/Patient.read")
	allowed, _ := smart.ParseScopes("system/*.read")
	if !granted.SubsetOf(allowed) {
		t.Fatal("specific should be subset of wildcard")
	}
	if allowed.SubsetOf(granted) {
		t.Fatal("wildcard should not be subset of specific")
	}
}

func unsignedJWT(t *testing.T, payload map[string]any) string {
	t.Helper()
	// Structural validation accepts an unsigned test token only when the
	// algorithm is a real JWS algorithm; alg=none is always rejected.
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	// Keep a non-empty placeholder signature so the compact JWT remains
	// structurally valid; tests using this helper intentionally omit a verifier.
	return header + "." + base64.RawURLEncoding.EncodeToString(body) + ".AA"
}

func mustRSAKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return key, string(pemBytes)
}

func signedJWT(t *testing.T, key *rsa.PrivateKey, payload map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	payloadSeg := base64.RawURLEncoding.EncodeToString(body)
	signingInput := header + "." + payloadSeg
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestParseScopes_Empty(t *testing.T) {
	set, err := smart.ParseScopes("   ")
	if err != nil {
		t.Fatal(err)
	}
	if !set.Empty() {
		t.Fatalf("expected empty, got %v", set.Strings())
	}
}

func TestClientRegistration_MinimalType(t *testing.T) {
	reg := smart.ClientRegistration{
		ClientID:     "app-1",
		RedirectURIs: []string{"https://app.example/callback"},
		GrantTypes:   []string{"authorization_code"},
		Scopes:       []string{"patient/*.read"},
	}
	if reg.ClientID == "" || !strings.Contains(reg.RedirectURIs[0], "https") {
		t.Fatalf("reg = %#v", reg)
	}
}
