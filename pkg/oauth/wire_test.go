package oauth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/client"
	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

func TestMultiTenantBearerAuthResolvesTenantIssuer(t *testing.T) {
	baseKey, err := oauth.NewKeySet(2048)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	baseIssuer := strings.TrimSuffix(ts.URL, "")
	liveTenantIssuer := baseIssuer + "/t/local"
	registry := oauth.NewTenantRegistry()
	if err := registry.Register(oauth.TenantIssuerConfig{
		TenantID:     "local",
		Issuer:       liveTenantIssuer,
		FHIRAudience: baseIssuer,
		SigningKey:   baseKey,
		AutoApprove:  boolPtr(true),
	}); err != nil {
		t.Fatal(err)
	}
	baseCfg := oauth.Config{
		Issuer:       baseIssuer,
		FHIRAudience: baseIssuer,
		SigningKey:   baseKey,
		AutoApprove:  true,
	}
	baseSrv, err := oauth.NewServer(baseCfg)
	if err != nil {
		t.Fatal(err)
	}
	multi, err := oauth.NewMultiTenantServer(oauth.MultiTenantConfig{Base: baseCfg, Tenants: registry})
	if err != nil {
		t.Fatal(err)
	}
	tenantSrv, err := multi.ServerForTenant("local")
	if err != nil {
		t.Fatal(err)
	}
	_ = tenantSrv.RegisterClient(oauth.Client{
		ClientID:                "tenant-client",
		ClientSecret:            "tenant-secret",
		TokenEndpointAuthMethod: oauth.AuthMethodClientSecretPost,
		RedirectURIs:            []string{"http://127.0.0.1/callback"},
		Scopes:                  []string{"patient/Patient.read"},
	})

	mux.Handle("/t/", multi.Handler())
	httpClient, err := client.New(client.Config{BaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	pkce, _ := client.NewPKCEChallenge()
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: &client.SMARTConfiguration{
			AuthorizationEndpoint: liveTenantIssuer + "/oauth/authorize",
			TokenEndpoint:         liveTenantIssuer + "/oauth/token",
		},
		ClientID: "tenant-client", RedirectURI: "http://127.0.0.1/callback",
		Scope: "patient/Patient.read", PKCE: pkce,
	})
	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authResp, err := noRedirect.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = authResp.Body.Close()
	if authResp.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d", authResp.StatusCode)
	}
	location := authResp.Header.Get("Location")
	code := strings.Split(strings.Split(location, "code=")[1], "&")[0]
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", "http://127.0.0.1/callback")
	form.Set("client_id", "tenant-client")
	form.Set("client_secret", "tenant-secret")
	form.Set("code_verifier", pkce.Verifier)
	tokenResp, err := http.PostForm(liveTenantIssuer+"/oauth/token", form)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tokenResp.Body.Close() }()
	if tokenResp.StatusCode != http.StatusOK {
		t.Fatalf("token status = %d", tokenResp.StatusCode)
	}
	var tokenDoc map[string]any
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenDoc); err != nil {
		t.Fatal(err)
	}
	accessToken, _ := tokenDoc["access_token"].(string)
	if accessToken == "" {
		t.Fatalf("token doc = %#v", tokenDoc)
	}

	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{DefaultTenantID: "local"})
	mtAuth := oauth.MultiTenantBearerAuth{Base: baseSrv, Tenants: multi, Adapter: adapter}
	req := httptest.NewRequest(http.MethodGet, "/fhir/Patient", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	principal, tenant, err := mtAuth.PrincipalResolver()(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if principal.ID == "" || tenant.TenantID == "" {
		t.Fatalf("principal=%#v tenant=%#v", principal, tenant)
	}
	cfg, err := mtAuth.BearerConfigForRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Options.ExpectedIssuer != liveTenantIssuer {
		t.Fatalf("issuer = %q", cfg.Options.ExpectedIssuer)
	}
}

func TestRegisterRejectsInvalidRedirectURI(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := strings.TrimSuffix(srv.URL, "/")
	server, err := oauth.NewServer(oauth.Config{
		Issuer:                   base,
		FHIRAudience:             base,
		AllowDynamicRegistration: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	mux.Handle("/", server.Handler())

	body := `{"redirect_uris":["http://app.example/callback"],"grant_types":["authorization_code"]}`
	resp, err := http.Post(base+"/oauth/register", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
