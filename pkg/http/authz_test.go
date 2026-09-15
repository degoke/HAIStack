package http_test

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
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/core"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/smart"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/testkit/golden"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestHTTPAuthz_BearerTokenExpired(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	bearer := smartBearerConfig(now)
	token := unsignedJWT(map[string]any{
		"iss": "https://issuer.example", "sub": "user-1", "aud": "https://aud.example",
		"exp": now.Add(-time.Hour).Unix(), "scope": "patient/Observation.rs",
	})
	handler := newBearerAuthHandler(t, bearer, &fakeResourceService{}, nil, nil)
	rec := doRequestWithHeaders(t, handler, http.MethodGet, "/fhir/Observation/obs-1", nil, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	outcome := decodeOutcome(t, rec.Body.Bytes())
	golden.AssertOutcomeEqual(t, outcome, golden.AuthOutcomeCatalog["security_token_expired"])
}

func TestHTTPAuthz_ScopeFilterReadDenied(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	bearer := smartBearerConfig(now)
	token := validPatientObservationScopeToken(now, "pat-1")
	obs := observationEnvelope("obs-vital", "vital-signs")
	handler := newBearerAuthHandler(t, bearer, &fakeResourceService{
		readFn: func(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
			if resourceType == "Observation" && id == obs.ID {
				return obs, nil
			}
			return nil, &core.ServiceError{Kind: core.ErrorKindNotFound, Message: "not found"}
		},
	}, nil, nil)
	rec := doRequestWithHeaders(t, handler, http.MethodGet, "/fhir/Observation/"+obs.ID, nil, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	outcome := decodeOutcome(t, rec.Body.Bytes())
	golden.AssertOutcomeEqual(t, outcome, golden.AuthOutcomeCatalog["forbidden_scope_filter"])
}

func TestHTTPAuthz_ScopeFilterSearchPostFilter(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	bearer := smartBearerConfig(now)
	token := validUserObservationScopeToken(now)
	lab := observationEnvelope("obs-lab", "laboratory")
	vital := observationEnvelope("obs-vital", "vital-signs")
	searchSvc := &fakeSearchService{
		searchFn: func(_ context.Context, resourceType string, params url.Values) (*search.SearchBundle, error) {
			if params.Get("category") != "laboratory" {
				t.Fatalf("expected category filter injected, got %#v", params)
			}
			return search.AssembleBundle(&search.Result{
				ResourceType: resourceType,
				Resources:    []*types.ResourceEnvelope{lab, vital},
			}), nil
		},
	}
	handler := newBearerAuthHandler(t, bearer, &fakeResourceService{}, searchSvc, nil)
	rec := doRequestWithHeaders(t, handler, http.MethodGet, "/fhir/Observation", nil, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var bundle map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	entries, _ := bundle["entry"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected 1 filtered entry, got %d", len(entries))
	}
}

func TestHTTPAuthz_RevincludeBundleFiltered(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	bearer := smartBearerConfig(now)
	token := validUserObservationScopeToken(now)
	lab := observationEnvelope("obs-lab", "laboratory")
	vital := observationEnvelope("obs-vital", "vital-signs")
	searchSvc := &fakeSearchService{
		searchFn: func(_ context.Context, resourceType string, _ url.Values) (*search.SearchBundle, error) {
			return search.AssembleBundle(&search.Result{
				ResourceType: resourceType,
				Resources:    []*types.ResourceEnvelope{lab},
				Included: []search.IncludedEntry{{
					ResourceType: "Observation",
					ID:           vital.ID,
					Resource:     vital,
					Mode:         "include",
				}},
			}), nil
		},
	}
	handler := newBearerAuthHandler(t, bearer, &fakeResourceService{}, searchSvc, nil)
	rec := doRequestWithHeaders(t, handler, http.MethodGet, "/fhir/Observation?_revinclude=Observation:subject", nil, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var bundle map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &bundle)
	entries, _ := bundle["entry"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected revincluded out-of-filter observation removed, got %d entries", len(entries))
	}
}

func TestHTTPAuthz_EverythingBundleFiltered(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	bearer := smartBearerConfig(now)
	token := validEverythingScopeToken(now, "pat-1")
	lab := observationEnvelope("obs-lab", "laboratory")
	vital := observationEnvelope("obs-vital", "vital-signs")
	handler := newBearerAuthHandler(t, bearer, &fakeResourceService{}, nil, everythingBundleEnvelope(lab, vital))
	rec := doRequestWithHeaders(t, handler, http.MethodGet, "/fhir/Patient/pat-1/$everything", nil, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var bundle map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &bundle)
	entries, _ := bundle["entry"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected filtered $everything bundle, got %d entries", len(entries))
	}
}

func TestHTTPAuthz_HistoryFiltersOutOfScopeVersions(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	bearer := smartBearerConfig(now)
	token := validPatientObservationScopeToken(now, "pat-1")
	lab := observationEnvelope("obs-lab", "laboratory")
	vital := observationEnvelope("obs-lab", "vital-signs")
	handler := newBearerAuthHandler(t, bearer, &fakeResourceService{
		readFn: func(_ context.Context, _, _ string) (*types.ResourceEnvelope, error) { return lab, nil },
		historyFn: func(_ context.Context, _, _ string) ([]store.ResourceVersion, error) {
			return []store.ResourceVersion{
				{VersionID: "1", Resource: lab, Action: store.VersionActionCreate},
				{VersionID: "2", Resource: vital, Action: store.VersionActionUpdate},
			}, nil
		},
	}, nil, nil)
	rec := doRequestWithHeaders(t, handler, http.MethodGet, "/fhir/Observation/obs-lab/_history", nil, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var bundle map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &bundle)
	entries, _ := bundle["entry"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected 1 history version after scope filter, got %d", len(entries))
	}
}

func TestHTTPAuthz_ReadOnlyScopeDeniesSearch(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	bearer := smartBearerConfig(now)
	token := unsignedJWT(map[string]any{
		"iss": "https://issuer.example", "sub": "user-1", "aud": "https://aud.example",
		"exp":   now.Add(time.Hour).Unix(),
		"scope": "user/Observation.r?category=laboratory",
	})
	handler := newBearerAuthHandler(t, bearer, &fakeResourceService{}, &fakeSearchService{}, nil)
	rec := doRequestWithHeaders(t, handler, http.MethodGet, "/fhir/Observation", nil, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestHTTPAuthz_BackendAssertionReplayRejected(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	key, pemPub := mustRSAKey(t)
	client := smart.BackendClient{
		ClientID: "backend-app", AllowedScopes: []string{"system/*.read"},
		Key: smart.ClientKeyMetadata{Algorithm: "RS256", PublicKeyPEM: pemPub},
	}
	bsa, err := smart.NewBackendServiceAuth("https://auth.example/token", client)
	if err != nil {
		t.Fatal(err)
	}
	bsa.Now = func() time.Time { return now }
	assertion := signedJWTAuthz(t, key, map[string]any{
		"iss": "backend-app", "sub": "backend-app", "aud": "https://auth.example/token",
		"exp": now.Add(5 * time.Minute).Unix(), "jti": "assertion-replay-http", "scope": "system/*.read",
	})
	if _, _, err := bsa.ValidateBackendAssertion(assertion, smart.TokenValidateOptions{}); err != nil {
		t.Fatal(err)
	}
	backend := smart.BackendAssertionAuthConfig{
		Backend: bsa,
		Adapter: smart.NewAuthAdapter(smart.AuthAdapterConfig{
			DefaultTenantID: "tenant-svc", DefaultServiceRoles: []string{"backend"},
		}),
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{
			readFn: func(_ context.Context, _, _ string) (*types.ResourceEnvelope, error) {
				return patientEnvelope("pat-1", "Doe"), nil
			},
		},
		PrincipalResolver:  hahttp.SMARTBackendAssertionPrincipalResolver(backend),
		AuthBundleResolver: hahttp.SMARTBackendAssertionBundleResolver(backend),
		AuthChecker:        smart.ScopePolicyAuthChecker{Engine: mustAuthEngine(t), Adapter: backend.Adapter},
	})
	rec := doRequestWithHeaders(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil, map[string]string{
		"Authorization": "Bearer " + assertion,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	outcome := decodeOutcome(t, rec.Body.Bytes())
	golden.AssertOutcomeEqual(t, outcome, golden.AuthOutcomeCatalog["security_replay"])
}

func TestHTTPAuthz_BearerTokenNotYetValid(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	bearer := smartBearerConfig(now)
	token := unsignedJWT(map[string]any{
		"iss": "https://issuer.example", "sub": "user-1", "aud": "https://aud.example",
		"nbf": now.Add(time.Hour).Unix(), "exp": now.Add(2 * time.Hour).Unix(),
		"scope": "patient/Observation.rs",
	})
	handler := newBearerAuthHandler(t, bearer, &fakeResourceService{}, nil, nil)
	rec := doRequestWithHeaders(t, handler, http.MethodGet, "/fhir/Observation/obs-1", nil, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	outcome := decodeOutcome(t, rec.Body.Bytes())
	golden.AssertOutcomeEqual(t, outcome, golden.AuthOutcomeCatalog["security_token_not_yet_valid"])
}

func TestHTTPAuthz_UnauthenticatedGolden(t *testing.T) {
	handler := newBearerAuthHandler(t, smartBearerConfig(time.Now()), &fakeResourceService{}, nil, nil)
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	outcome := decodeOutcome(t, rec.Body.Bytes())
	golden.AssertOutcomeEqual(t, outcome, golden.AuthOutcomeCatalog["security_unauthenticated"])
}

func TestHTTPAuthz_WriteDeniedForOutOfFilterObservation(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	bearer := smartBearerConfig(now)
	token := unsignedJWT(map[string]any{
		"iss": "https://issuer.example", "sub": "user-1", "aud": "https://aud.example",
		"exp":   now.Add(time.Hour).Unix(),
		"scope": "user/Observation.c?category=laboratory",
	})
	body := observationJSON("obs-new", "vital-signs")
	handler := newBearerAuthHandler(t, bearer, &fakeResourceService{
		createFn: func(_ context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
			return resource, nil
		},
	}, nil, nil)
	rec := doRequestWithHeaders(t, handler, http.MethodPost, "/fhir/Observation", body, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func smartBearerConfig(now time.Time) smart.BearerAuthConfig {
	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{
		DefaultTenantID:  "tenant-a",
		DefaultUserRoles: []string{"clinician"},
	})
	tv := smart.NewTokenValidator(nil)
	tv.Now = func() time.Time { return now }
	return smart.BearerAuthConfig{
		Validator: tv,
		Adapter:   adapter,
		Options: smart.TokenValidateOptions{
			ExpectedIssuer:   "https://issuer.example",
			ExpectedAudience: "https://aud.example",
		},
	}
}

func newBearerAuthHandler(t *testing.T, bearer smart.BearerAuthConfig, svc *fakeResourceService, searchSvc hahttp.SearchService, everything *types.ResourceEnvelope) http.Handler {
	t.Helper()
	if svc == nil {
		svc = &fakeResourceService{}
	}
	if searchSvc == nil {
		searchSvc = &fakeSearchService{}
	}
	eng, err := auth.NewEngine(auth.Config{
		Roles: []auth.Role{{
			Name: "clinician", Permissions: []auth.Permission{"*.read", "*.write", "Patient.read", "Observation.read", "observation.read", "observation.write"},
		}},
		PolicyBytes: []byte(`{
			"version":"1",
			"rules":[
				{"name":"read","effect":"allow","match":{"actions":["read"],"anyPermissions":["*.read"]}},
				{"name":"write","effect":"allow","match":{"actions":["write"],"anyPermissions":["*.write"]}},
				{"name":"patient","effect":"allow","match":{"actions":["patient-access"]}}
			]
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var opSvc hahttp.OperationService
	if everything != nil {
		opSvc = &staticEverythingService{bundle: everything}
	}
	return newTestHandler(t, hahttp.Config{
		ResourceService:    svc,
		SearchService:      searchSvc,
		OperationService:   opSvc,
		PrincipalResolver:  hahttp.SMARTBearerPrincipalResolver(bearer),
		AuthBundleResolver: hahttp.SMARTBearerBundleResolver(bearer),
		AuthChecker: smart.ScopePolicyAuthChecker{
			Engine: eng, Adapter: bearer.Adapter,
		},
	})
}

type staticEverythingService struct {
	bundle *types.ResourceEnvelope
}

func (s *staticEverythingService) Execute(_ context.Context, req hahttp.OperationRequest) (*types.ResourceEnvelope, error) {
	if req.Operation == "$everything" {
		return s.bundle, nil
	}
	return nil, nil
}

func validPatientObservationScopeToken(now time.Time, patientID string) string {
	return unsignedJWT(map[string]any{
		"iss": "https://issuer.example", "sub": "user-1", "aud": "https://aud.example",
		"exp":     now.Add(time.Hour).Unix(),
		"scope":   "launch/patient patient/Observation.rs?category=laboratory",
		"patient": patientID,
	})
}

func validUserObservationScopeToken(now time.Time) string {
	return unsignedJWT(map[string]any{
		"iss": "https://issuer.example", "sub": "user-1", "aud": "https://aud.example",
		"exp":   now.Add(time.Hour).Unix(),
		"scope": "user/Observation.rs?category=laboratory",
	})
}

func validEverythingScopeToken(now time.Time, patientID string) string {
	return unsignedJWT(map[string]any{
		"iss": "https://issuer.example", "sub": "user-1", "aud": "https://aud.example",
		"exp":     now.Add(time.Hour).Unix(),
		"scope":   "launch/patient patient/Patient.rs patient/Observation.rs?category=laboratory",
		"patient": patientID,
	})
}

func unsignedJWT(payload map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	body, _ := json.Marshal(payload)
	return header + "." + base64.RawURLEncoding.EncodeToString(body) + ".AA"
}

func observationEnvelope(id, category string) *types.ResourceEnvelope {
	data := observationJSON(id, category)
	env, err := types.NewJSONCodec().ParseJSON("Observation", data)
	if err != nil {
		panic(err)
	}
	return env
}

func observationJSON(id, category string) []byte {
	payload := map[string]any{
		"resourceType": "Observation",
		"id":           id,
		"status":       "final",
		"category": []map[string]any{{
			"coding": []map[string]string{{
				"system": "http://terminology.hl7.org/CodeSystem/observation-category",
				"code":   category,
			}},
		}},
	}
	data, _ := json.Marshal(payload)
	return data
}

func mustAuthEngine(t *testing.T) *auth.Engine {
	eng, err := auth.NewEngine(auth.Config{
		Roles:       []auth.Role{{Name: "backend", Permissions: []auth.Permission{"*.read"}}},
		PolicyBytes: []byte(`{"version":"1","rules":[{"name":"read","effect":"allow","match":{"actions":["read"],"anyPermissions":["*.read"]}}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return eng
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
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func signedJWTAuthz(t *testing.T, key *rsa.PrivateKey, payload map[string]any) string {
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

func everythingBundleEnvelope(resources ...*types.ResourceEnvelope) *types.ResourceEnvelope {
	entries := make([]map[string]any, 0, len(resources))
	for _, res := range resources {
		var obj map[string]any
		_ = json.Unmarshal(res.JSON, &obj)
		entries = append(entries, map[string]any{"resource": obj})
	}
	payload := map[string]any{"resourceType": "Bundle", "type": "searchset", "entry": entries}
	data, _ := json.Marshal(payload)
	env, _ := types.NewJSONCodec().ParseJSON("Bundle", data)
	return env
}
