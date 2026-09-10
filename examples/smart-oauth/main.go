package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/degoke/health-ai-stack/examples/internal/appkit"
	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/client"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "smart-oauth: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "haistack-smart-oauth-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	stack, err := appkit.NewSQLiteStack(ctx, filepath.Join(tempDir, "smart.db"), "Patient")
	if err != nil {
		return err
	}
	defer stack.Close()

	patient, err := appkit.EnvelopeFromJSON("Patient", appkit.PatientJSON("OAuth", "Demo", "+1-555-0200"))
	if err != nil {
		return err
	}
	created, err := stack.ResourceService.Create(ctx, patient)
	if err != nil {
		return err
	}

	asMux := http.NewServeMux()
	asServer := httptest.NewServer(asMux)
	defer asServer.Close()
	issuer := asServer.URL

	authStore, err := oauth.NewFileAuthorizationStore(filepath.Join(tempDir, "oauth-tokens.json"))
	if err != nil {
		return err
	}
	oauthServer, err := oauth.NewServer(oauth.Config{
		Issuer:             issuer,
		FHIRAudience:       issuer,
		AuthorizationStore: authStore,
		RequireConsentForm: true,
	})
	if err != nil {
		return err
	}
	oauthServer.RegisterClient(oauth.Client{
		ClientID:     "demo-app",
		RedirectURIs: []string{"https://localhost/callback"},
		Scopes:       []string{"patient/Patient.rs"},
	})
	asMux.Handle("/", oauthServer.Handler())

	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{
		DefaultTenantID:  "demo",
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
		return err
	}
	patientRefResolver := &registry.PatientReferenceResolver{Snapshot: stack.Snapshot, Engine: stack.FHIRPath}
	scopeMatcher := smart.RegistryScopeFilterMatcherChain(stack.SearchRegistry, stack.FHIRPath)
	fhirHandler, err := hahttp.NewHandler(hahttp.Config{
		ResourceService:          hahttp.CoreResourceService{Svc: stack.ResourceService},
		SearchService:            hahttp.SearchServiceAdapter{Svc: stack.SearchService, PatientSearchParamResolver: stack.Snapshot},
		CapabilitySource:         hahttp.RegistryCapabilitySource{Snapshot: stack.Snapshot},
		PatientReferenceResolver: patientRefResolver,
		ScopeFilterMatcher:       scopeMatcher,
		PrincipalResolver:        hahttp.SMARTBearerPrincipalResolver(bearer),
		AuthBundleResolver:       hahttp.SMARTBearerBundleResolver(bearer),
		AuthChecker:              smart.ScopePolicyAuthChecker{Engine: engine, Adapter: adapter},
	})
	if err != nil {
		return err
	}
	fhirServer := httptest.NewServer(fhirHandler)
	defer fhirServer.Close()

	httpClient, err := client.New(client.Config{BaseURL: issuer})
	if err != nil {
		return err
	}
	cfg, err := httpClient.SMART().Discover(ctx, issuer)
	if err != nil {
		return err
	}
	pkce, err := client.NewPKCEChallenge()
	if err != nil {
		return err
	}
	authURL, err := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "demo-app", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", State: "demo", PKCE: pkce,
	})
	if err != nil {
		return err
	}
	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	redirectURL, err := followConsentAndAuthorize(noRedirect, authURL)
	if err != nil {
		return err
	}
	code := queryValue(redirectURL, "code")
	tokenResp, err := httpClient.SMART().ExchangeAuthCode(ctx, cfg.TokenEndpoint, "demo-app", "https://localhost/callback", code, pkce)
	if err != nil {
		return err
	}

	fhirClient, err := client.New(client.Config{
		BaseURL: fhirServer.URL, TokenProvider: client.TokenProviderFromResponse(tokenResp),
	})
	if err != nil {
		return err
	}
	envelope, err := fhirClient.Read(ctx, "Patient", created.ID)
	if err != nil {
		return err
	}

	fmt.Println("SMART OAuth + FHIR demo")
	fmt.Printf("issuer: %s\n", issuer)
	fmt.Printf("fhir: %s\n", fhirServer.URL)
	fmt.Printf("read Patient/%s via issued token: ok\n", envelope.ID)
	return nil
}

func followConsentAndAuthorize(client *http.Client, authURL string) (string, error) {
	resp, err := client.Get(authURL)
	if err != nil {
		return "", err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		return "", fmt.Errorf("authorize status = %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if strings.Contains(loc, "/oauth/consent") {
		consentResp, err := client.Post(loc, "application/x-www-form-urlencoded", strings.NewReader("approve=yes"))
		if err != nil {
			return "", err
		}
		io.Copy(io.Discard, consentResp.Body)
		consentResp.Body.Close()
		if consentResp.StatusCode != http.StatusFound {
			return "", fmt.Errorf("consent status = %d", consentResp.StatusCode)
		}
		loc = consentResp.Header.Get("Location")
	}
	return loc, nil
}

func queryValue(rawURL, key string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}
