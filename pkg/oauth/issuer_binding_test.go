package oauth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/client"
	"github.com/degoke/health-ai-stack/pkg/oauth"
)

func TestRequireBoundIssuer_RejectsEmpty(t *testing.T) {
	if _, err := oauth.RequireBoundIssuer(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestMemoryStore_RequiresIssuerOnSave(t *testing.T) {
	store := oauth.NewMemoryAuthorizationStore()
	if err := store.SaveAuthorizationCode("code", oauth.AuthorizationCode{
		ClientID: "client", ExpiresAt: time.Now().Add(time.Minute),
	}); err == nil {
		t.Fatal("expected issuer required")
	}
	if err := store.SaveRefreshToken("rt", oauth.RefreshTokenEntry{
		ClientID: "client", ExpiresAt: time.Now().Add(time.Minute),
	}); err == nil {
		t.Fatal("expected issuer required")
	}
	if err := store.SavePendingAuthorization("p", oauth.PendingAuthorization{
		ExpiresAt: time.Now().Add(time.Minute),
	}); err == nil {
		t.Fatal("expected issuer required")
	}
}

func TestMemoryStore_IsolatesSameCodeAcrossIssuers(t *testing.T) {
	store := oauth.NewMemoryAuthorizationStore()
	issuerA := "https://auth.example/t/clinic-a"
	issuerB := "https://auth.example/t/clinic-b"
	now := time.Now().Add(5 * time.Minute)
	if err := store.SaveAuthorizationCode("same-code", oauth.AuthorizationCode{
		Issuer: issuerA, ClientID: "a", RedirectURI: "https://a/cb", ExpiresAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAuthorizationCode("same-code", oauth.AuthorizationCode{
		Issuer: issuerB, ClientID: "b", RedirectURI: "https://b/cb", ExpiresAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	entryA, ok := store.ConsumeAuthorizationCode(issuerA, "same-code")
	if !ok || entryA.ClientID != "a" {
		t.Fatalf("issuer A = %+v ok=%v", entryA, ok)
	}
	entryB, ok := store.ConsumeAuthorizationCode(issuerB, "same-code")
	if !ok || entryB.ClientID != "b" {
		t.Fatalf("issuer B = %+v ok=%v", entryB, ok)
	}
}

func TestMemoryStore_ConsumeDeletesExpired(t *testing.T) {
	store := oauth.NewMemoryAuthorizationStore()
	const issuer = "https://auth.example.test"
	if err := store.SaveAuthorizationCode("expired", oauth.AuthorizationCode{
		Issuer: issuer, ClientID: "c", ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.ConsumeAuthorizationCode(issuer, "expired"); ok {
		t.Fatal("expected expired consume to fail")
	}
	if err := store.SaveAuthorizationCode("expired", oauth.AuthorizationCode{
		Issuer: issuer, ClientID: "fresh", ExpiresAt: time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	entry, ok := store.ConsumeAuthorizationCode(issuer, "expired")
	if !ok || entry.ClientID != "fresh" {
		t.Fatalf("expected expired row to have been removed: %+v ok=%v", entry, ok)
	}
}

func TestEntryIssuerMatches_RequiresNonEmptyIssuer(t *testing.T) {
	if oauth.EntryIssuerMatches("", "https://auth.example") {
		t.Fatal("empty entry issuer must not match")
	}
	if oauth.EntryIssuerMatches("https://auth.example", "") {
		t.Fatal("empty server issuer must not match")
	}
}

func TestIssuerBinding_RejectsCrossIssuerAuthorizationCode(t *testing.T) {
	store := oauth.NewMemoryAuthorizationStore()
	issuerA := "https://auth.example/t/clinic-a"
	issuerB := "https://auth.example/t/clinic-b"
	now := time.Now()

	code := "shared-code-test"
	if err := store.SaveAuthorizationCode(code, oauth.AuthorizationCode{
		Issuer:      issuerA,
		ClientID:    "client",
		RedirectURI: "https://app/cb",
		Scope:       "openid",
		ExpiresAt:   now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.ConsumeAuthorizationCode(issuerB, code); ok {
		t.Fatal("expected cross-issuer code consume to fail")
	}
	entry, ok := store.ConsumeAuthorizationCode(issuerA, code)
	if !ok || entry.ClientID != "client" {
		t.Fatalf("expected code for issuer A: ok=%v entry=%+v", ok, entry)
	}
}

func TestIssuerBinding_RejectsCrossIssuerRefreshToken(t *testing.T) {
	store := oauth.NewMemoryAuthorizationStore()
	issuerA := "https://auth.example/t/clinic-a"
	issuerB := "https://auth.example/t/clinic-b"
	token := "refresh-1"
	if err := store.SaveRefreshToken(token, oauth.RefreshTokenEntry{
		Issuer:    issuerA,
		ClientID:  "app",
		Subject:   "sub",
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.ConsumeRefreshToken(issuerB, token); ok {
		t.Fatal("expected cross-issuer refresh consume to fail")
	}
	if _, ok := store.ConsumeRefreshToken(issuerA, token); !ok {
		t.Fatal("expected refresh for issuer A")
	}
}

func TestIssuerBinding_MultiTenantAuthorizeTokenExchange(t *testing.T) {
	store := oauth.NewMemoryAuthorizationStore()
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	base := strings.TrimSuffix(ts.URL, "")
	issuerA := base + "/t/clinic-a"
	issuerB := base + "/t/clinic-b"

	key, err := oauth.NewKeySet(2048)
	if err != nil {
		t.Fatal(err)
	}
	registry := oauth.NewTenantRegistry()
	for _, spec := range []struct{ id, iss string }{
		{"clinic-a", issuerA},
		{"clinic-b", issuerB},
	} {
		if err := registry.Register(oauth.TenantIssuerConfig{
			TenantID:     spec.id,
			Issuer:       spec.iss,
			FHIRAudience: base,
		}); err != nil {
			t.Fatal(err)
		}
	}
	multi, err := oauth.NewMultiTenantServer(oauth.MultiTenantConfig{
		Base: oauth.Config{
			SigningKey:         key,
			AuthorizationStore: store,
			AutoApprove:        true,
		},
		Tenants: registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	mux.Handle("/t/", multi.Handler())

	srvA, err := multi.ServerForTenant("clinic-a")
	if err != nil {
		t.Fatal(err)
	}
	clientID := "app"
	clientSecret := "secret"
	registerApp := func(srv *oauth.Server) {
		t.Helper()
		if err := srv.RegisterClient(oauth.Client{
			ClientID:                clientID,
			ClientSecret:            clientSecret,
			TokenEndpointAuthMethod: oauth.AuthMethodClientSecretPost,
			RedirectURIs:            []string{"https://app/cb"},
			Scopes:                  []string{"openid", "patient/Patient.rs"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	registerApp(srvA)

	httpClient, _ := client.New(client.Config{BaseURL: ts.URL})
	pkce, _ := client.NewPKCEChallenge()
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: &client.SMARTConfiguration{
			AuthorizationEndpoint: issuerA + "/oauth/authorize",
			TokenEndpoint:         issuerA + "/oauth/token",
		},
		ClientID: clientID, RedirectURI: "https://app/cb",
		Scope: "openid patient/Patient.rs", PKCE: pkce,
	})
	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authResp, err := noRedirect.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = authResp.Body.Close()
	code := strings.Split(strings.Split(authResp.Header.Get("Location"), "code=")[1], "&")[0]

	srvB, err := multi.ServerForTenant("clinic-b")
	if err != nil {
		t.Fatal(err)
	}
	registerApp(srvB)
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"https://app/cb"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code_verifier": {pkce.Verifier},
	}
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srvB.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("tenant B token status = %d body=%s", rec.Code, rec.Body.String())
	}

	tokenResp, err := http.PostForm(issuerA+"/oauth/token", form)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tokenResp.Body.Close() }()
	if tokenResp.StatusCode != http.StatusOK {
		t.Fatalf("tenant A token status = %d", tokenResp.StatusCode)
	}
	var tok map[string]any
	if err := json.NewDecoder(tokenResp.Body).Decode(&tok); err != nil {
		t.Fatal(err)
	}
	refresh, _ := tok["refresh_token"].(string)
	if refresh == "" {
		t.Fatal("missing refresh_token")
	}

	refreshForm := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}
	badRefresh, err := http.PostForm(issuerB+"/oauth/token", refreshForm)
	if err != nil {
		t.Fatal(err)
	}
	_ = badRefresh.Body.Close()
	if badRefresh.StatusCode != http.StatusBadRequest {
		t.Fatalf("cross-tenant refresh status = %d", badRefresh.StatusCode)
	}
}

func TestIssuerBinding_ConsentSessionScopedToIssuer(t *testing.T) {
	store := oauth.NewMemoryAuthorizationStore()
	base := "https://auth.example"
	issuerA := base + "/t/clinic-a"
	issuerB := base + "/t/clinic-b"

	srvA, err := oauth.NewServer(oauth.Config{
		Issuer:             issuerA,
		FHIRAudience:       base,
		AuthorizationStore: store,
		RequireConsentForm: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = srvA.RegisterClient(oauth.Client{
		ClientID: "c", RedirectURIs: []string{"https://app/cb"}, Scopes: []string{"openid"},
	})
	srvB, err := oauth.NewServer(oauth.Config{
		Issuer:             issuerB,
		FHIRAudience:       base,
		AuthorizationStore: store,
		RequireConsentForm: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = srvB.RegisterClient(oauth.Client{
		ClientID: "c", RedirectURIs: []string{"https://app/cb"}, Scopes: []string{"openid"},
	})

	sessionID := "consent-sess-1"
	if err := store.SavePendingAuthorization(sessionID, oauth.PendingAuthorization{
		Issuer:    issuerA,
		Request:   oauth.AuthorizationRequest{ClientID: "c", RedirectURI: "https://app/cb", Scope: "openid"},
		CSRFToken: "csrf",
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/oauth/consent?id="+sessionID, nil)
	rec := httptest.NewRecorder()
	srvB.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cross-issuer consent GET status = %d", rec.Code)
	}
	req2 := httptest.NewRequest("GET", "/oauth/consent?id="+sessionID, nil)
	rec2 := httptest.NewRecorder()
	srvA.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("issuer A consent GET status = %d", rec2.Code)
	}
}
