package http_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/client"
	"github.com/degoke/health-ai-stack/pkg/core"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

type infernoEnv struct {
	issuer  string
	fhirURL string
	svc     *core.ResourceService
}

func setupInfernoEnv(t *testing.T) *infernoEnv {
	t.Helper()
	_, svc := openIntegrationStack(t)
	mux := http.NewServeMux()
	as := httptest.NewServer(mux)
	t.Cleanup(as.Close)
	issuer := strings.TrimSuffix(as.URL, "/")

	oauthServer, err := oauth.NewServer(oauth.Config{Issuer: issuer, FHIRAudience: issuer, AutoApprove: true})
	if err != nil {
		t.Fatal(err)
	}
	oauthServer.RegisterClient(oauth.Client{
		ClientID:     "inferno-client",
		RedirectURIs: []string{"https://localhost/callback"},
		Scopes:       []string{"patient/Patient.rs"},
	})
	mux.Handle("/", oauthServer.Handler())

	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{
		DefaultTenantID:  "inferno",
		DefaultUserRoles: []string{"clinician"},
	})
	bearer := oauthServer.BearerAuthConfig(adapter)
	engine, err := auth.NewEngine(auth.Config{
		Roles: []auth.Role{{Name: "clinician", Permissions: []auth.Permission{"*.read", "Patient.read"}}},
		PolicyBytes: []byte(`{
			"version":"1",
			"rules":[{"name":"read","effect":"allow","match":{"actions":["read"],"anyPermissions":["*.read"]}}]
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	secured, err := hahttp.NewHandler(hahttp.Config{
		ResourceService:    hahttp.CoreResourceService{Svc: svc},
		PrincipalResolver:  hahttp.SMARTBearerPrincipalResolver(bearer),
		AuthBundleResolver: hahttp.SMARTBearerBundleResolver(bearer),
		AuthChecker:        smart.ScopePolicyAuthChecker{Engine: engine, Adapter: adapter},
	})
	if err != nil {
		t.Fatal(err)
	}
	fhirServer := httptest.NewServer(secured)
	t.Cleanup(fhirServer.Close)
	return &infernoEnv{issuer: issuer, fhirURL: fhirServer.URL, svc: svc}
}

func (e *infernoEnv) authorizePKCE(t *testing.T, ctx context.Context, scope string) *client.TokenResponse {
	t.Helper()
	httpClient, err := client.New(client.Config{BaseURL: e.issuer})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := httpClient.SMART().Discover(ctx, e.issuer)
	if err != nil {
		t.Fatal(err)
	}
	pkce, err := client.NewPKCEChallenge()
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "inferno-client", RedirectURI: "https://localhost/callback",
		Scope: scope, State: "inferno", PKCE: pkce,
	})
	if err != nil {
		t.Fatal(err)
	}
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
	loc := authResp.Header.Get("Location")
	code := strings.Split(strings.Split(loc, "code=")[1], "&")[0]
	tokenResp, err := httpClient.SMART().ExchangeAuthCode(ctx, client.AuthCodeExchangeRequest{
		TokenEndpoint: cfg.TokenEndpoint,
		ClientID:      "inferno-client",
		RedirectURI:   "https://localhost/callback",
		Code:          code,
		PKCE:          pkce,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tokenResp
}

func TestInfernoStyleSMARTConformance(t *testing.T) {
	env := setupInfernoEnv(t)
	ctx := context.Background()
	patient := patientEnvelope("inferno-pat", "Patient")
	created, err := env.svc.Create(ctx, patient)
	if err != nil {
		t.Fatal(err)
	}
	tokenResp := env.authorizePKCE(t, ctx, "patient/Patient.rs")
	fhirClient, err := client.New(client.Config{
		BaseURL: env.fhirURL, TokenProvider: client.TokenProviderFromResponse(tokenResp),
	})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := fhirClient.Read(ctx, "Patient", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.ID != created.ID {
		t.Fatalf("patient = %+v", envelope)
	}
}

func TestInfernoStyleTokenRefresh(t *testing.T) {
	env := setupInfernoEnv(t)
	ctx := context.Background()
	tokenResp := env.authorizePKCE(t, ctx, "patient/Patient.rs")
	if tokenResp.RefreshToken == "" {
		t.Fatal("expected refresh token")
	}
	httpClient, err := client.New(client.Config{BaseURL: env.issuer})
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := httpClient.SMART().RefreshToken(ctx, client.RefreshTokenRequest{
		TokenEndpoint: env.issuer + "/oauth/token",
		ClientID:      "inferno-client",
		RefreshToken:  tokenResp.RefreshToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.AccessToken == "" {
		t.Fatal("missing refreshed access token")
	}
}

func TestInfernoStyleInvalidTokenRejected(t *testing.T) {
	env := setupInfernoEnv(t)
	ctx := context.Background()
	fhirClient, err := client.New(client.Config{
		BaseURL: env.fhirURL, TokenProvider: client.StaticTokenProvider{Token: "not-a-valid-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = fhirClient.Read(ctx, "Patient", "missing")
	if err == nil {
		t.Fatal("expected unauthorized read")
	}
}

func TestInfernoStyleRedirectURIRejected(t *testing.T) {
	env := setupInfernoEnv(t)
	ctx := context.Background()
	httpClient, err := client.New(client.Config{BaseURL: env.issuer})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := httpClient.SMART().Discover(ctx, env.issuer)
	if err != nil {
		t.Fatal(err)
	}
	pkce, _ := client.NewPKCEChallenge()
	authURL, err := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "inferno-client", RedirectURI: "https://evil.example/callback",
		Scope: "patient/Patient.rs", PKCE: pkce,
	})
	if err != nil {
		t.Fatal(err)
	}
	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authResp, err := noRedirect.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = authResp.Body.Close()
	if authResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("authorize status = %d", authResp.StatusCode)
	}
}

func TestInfernoStyleConsentFormEndToEnd(t *testing.T) {
	mux := http.NewServeMux()
	as := httptest.NewServer(mux)
	defer as.Close()
	issuer := strings.TrimSuffix(as.URL, "/")
	server, err := oauth.NewServer(oauth.Config{
		Issuer: issuer, FHIRAudience: issuer, RequireConsentForm: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterClient(oauth.Client{
		ClientID: "inferno-client", RedirectURIs: []string{"https://localhost/callback"},
		Scopes: []string{"patient/Patient.rs"},
	})
	mux.Handle("/", server.Handler())
	httpClient, _ := client.New(client.Config{BaseURL: issuer})
	cfg, _ := httpClient.SMART().Discover(context.Background(), issuer)
	pkce, _ := client.NewPKCEChallenge()
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "inferno-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", PKCE: pkce,
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
		t.Fatalf("expected consent redirect, status=%d", authResp.StatusCode)
	}
	consentURL := authResp.Header.Get("Location")
	consentPage, err := noRedirect.Get(consentURL)
	if err != nil {
		t.Fatal(err)
	}
	pageBody, _ := io.ReadAll(consentPage.Body)
	_ = consentPage.Body.Close()
	if consentPage.StatusCode != http.StatusOK || !strings.Contains(string(pageBody), "Authorize access") {
		t.Fatalf("consent page status=%d", consentPage.StatusCode)
	}
	csrf := extractConsentCSRF(string(pageBody))
	approveResp, err := noRedirect.Post(consentURL, "application/x-www-form-urlencoded",
		strings.NewReader("approve=yes&csrf_token="+url.QueryEscape(csrf)))
	if err != nil {
		t.Fatal(err)
	}
	_ = approveResp.Body.Close()
	if approveResp.StatusCode != http.StatusFound {
		t.Fatalf("approve status=%d", approveResp.StatusCode)
	}
	code := strings.Split(strings.Split(approveResp.Header.Get("Location"), "code=")[1], "&")[0]
	tokenResp, err := httpClient.SMART().ExchangeAuthCode(context.Background(), client.AuthCodeExchangeRequest{
		TokenEndpoint: cfg.TokenEndpoint,
		ClientID:      "inferno-client",
		RedirectURI:   "https://localhost/callback",
		Code:          code,
		PKCE:          pkce,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tokenResp.AccessToken == "" {
		t.Fatal("missing access token after consent")
	}
}

func extractConsentCSRF(html string) string {
	const marker = `name="csrf_token" value="`
	start := strings.Index(html, marker)
	if start < 0 {
		return ""
	}
	start += len(marker)
	end := strings.Index(html[start:], `"`)
	if end < 0 {
		return ""
	}
	return html[start : start+end]
}

func TestInfernoStyleConsentRequiredWithoutAutoApprove(t *testing.T) {
	mux := http.NewServeMux()
	as := httptest.NewServer(mux)
	defer as.Close()
	issuer := strings.TrimSuffix(as.URL, "/")
	server, err := oauth.NewServer(oauth.Config{Issuer: issuer, FHIRAudience: issuer})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterClient(oauth.Client{
		ClientID: "inferno-client", RedirectURIs: []string{"https://localhost/callback"},
		Scopes: []string{"patient/Patient.rs"},
	})
	mux.Handle("/", server.Handler())
	httpClient, _ := client.New(client.Config{BaseURL: issuer})
	cfg, _ := httpClient.SMART().Discover(context.Background(), issuer)
	pkce, _ := client.NewPKCEChallenge()
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "inferno-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", PKCE: pkce,
	})
	resp, err := (&http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}).Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected consent required, status=%d", resp.StatusCode)
	}
}
