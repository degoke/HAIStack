package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/client"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

// Inferno-style SMART on FHIR smoke: discovery, PKCE auth-code flow, and FHIR read with issued token.
func TestInfernoStyleSMARTConformance(t *testing.T) {
	ctx := context.Background()
	_, svc := openIntegrationStack(t)
	patient := patientEnvelope("inferno-pat", "Patient")
	created, err := svc.Create(ctx, patient)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	as := httptest.NewServer(mux)
	defer as.Close()
	issuer := strings.TrimSuffix(as.URL, "/")

	oauthServer, err := oauth.NewServer(oauth.Config{Issuer: issuer, FHIRAudience: issuer})
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
	defer fhirServer.Close()

	httpClient, err := client.New(client.Config{BaseURL: issuer})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := httpClient.SMART().Discover(ctx, issuer)
	if err != nil {
		t.Fatal(err)
	}
	pkce, err := client.NewPKCEChallenge()
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config:      cfg,
		ClientID:    "inferno-client",
		RedirectURI: "https://localhost/callback",
		Scope:       "patient/Patient.rs",
		State:       "inferno",
		PKCE:        pkce,
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
	tokenResp, err := httpClient.SMART().ExchangeAuthCode(ctx, cfg.TokenEndpoint, "inferno-client", "https://localhost/callback", code, pkce)
	if err != nil {
		t.Fatal(err)
	}

	fhirClient, err := client.New(client.Config{
		BaseURL:       fhirServer.URL,
		TokenProvider: client.TokenProviderFromResponse(tokenResp),
	})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := fhirClient.Read(ctx, "Patient", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if envelope == nil || envelope.ID != created.ID {
		t.Fatalf("patient = %+v", envelope)
	}
}
