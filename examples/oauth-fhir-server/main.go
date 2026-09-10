package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/degoke/health-ai-stack/examples/internal/appkit"
	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/client"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "oauth-fhir-server: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "haistack-oauth-fhir-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	stack, err := appkit.NewSQLiteStack(ctx, filepath.Join(tempDir, "oauth.db"), "Patient")
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	patient, err := appkit.EnvelopeFromJSON("Patient", appkit.PatientJSON("Demo", "Patient", "+1-555-0100"))
	if err != nil {
		return err
	}
	created, err := stack.ResourceService.Create(ctx, patient)
	if err != nil {
		return err
	}

	authEngine, err := auth.NewEngine(auth.Config{
		Roles: []auth.Role{{
			Name:        "clinician",
			Permissions: []auth.Permission{"Patient.read"},
		}},
		PolicyBytes: []byte(`{
  "version": "1",
  "rules": [{
    "name": "allow-patient-read",
    "effect": "allow",
    "match": {
      "actions": ["read"],
      "resourceTypes": ["Patient"],
      "anyPermissions": ["Patient.read"]
    },
    "reason": "clinicians may read patients"
  }]
}`),
		PolicyFormat: auth.PolicyFormatJSON,
	})
	if err != nil {
		return err
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	baseURL := "http://" + listener.Addr().String()
	fhirBase := baseURL + "/fhir"

	reg := oauth.NewClientRegistry()
	if err := reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "demo-app",
			RedirectURIs: []string{"http://127.0.0.1/callback"},
			Scopes:       []string{"patient/Patient.read", "launch/patient"},
		},
		DefaultPatient: created.ID,
		DefaultUser:    "Practitioner/demo",
		TenantHint:     "tenant-demo",
	}); err != nil {
		return err
	}

	oauthSrv, err := oauth.NewServer(oauth.Config{
		Issuer:      baseURL,
		FHIRBaseURL: fhirBase,
		Signer:      oauth.RS256Signer{PrivateKey: key, Kid: "demo"},
		Clients:     reg,
	})
	if err != nil {
		return err
	}
	wired, err := oauth.WireHTTP(oauth.WireConfig{
		Server: oauthSrv,
		Adapter: smart.NewAuthAdapter(smart.AuthAdapterConfig{
			DefaultTenantID:  "tenant-demo",
			DefaultUserRoles: []string{"clinician"},
		}),
	})
	if err != nil {
		return err
	}

	fhirHandler, err := hahttp.NewHandler(hahttp.Config{
		ResourceService: hahttp.CoreResourceService{Svc: stack.ResourceService},
		SearchService:   hahttp.SearchServiceAdapter{Svc: stack.SearchService},
		PrincipalResolver: wired.PrincipalResolver,
		AuthChecker: oauth.ScopePolicyAuthChecker{Engine: authEngine},
	})
	if err != nil {
		return err
	}
	root := oauth.MountRootHandler(fhirHandler, wired.OAuthHandler, nil, nil)

	srv := &http.Server{Handler: root}
	go func() { _ = srv.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	smartClient, err := client.New(client.Config{BaseURL: baseURL})
	if err != nil {
		return err
	}
	cfg, err := smartClient.SMART().Discover(ctx, baseURL)
	if err != nil {
		return err
	}
	pkce, err := client.NewPKCEChallenge()
	if err != nil {
		return err
	}
	authURL, err := smartClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config:      cfg,
		ClientID:    "demo-app",
		RedirectURI: "http://127.0.0.1/callback",
		Scope:       "patient/Patient.read launch/patient",
		State:       "demo",
		PKCE:        pkce,
		Aud:         fhirBase,
	})
	if err != nil {
		return err
	}
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.Get(authURL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	loc := resp.Header.Get("Location")
	code := queryValue(loc, "code")
	if code == "" {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("authorize failed: status=%d loc=%q body=%s", resp.StatusCode, loc, body)
	}
	tokenResp, err := smartClient.SMART().ExchangeAuthCode(ctx, cfg.TokenEndpoint, "demo-app", "http://127.0.0.1/callback", code, pkce)
	if err != nil {
		return err
	}

	unauthStatus, _ := head(baseURL + "/fhir/Patient/" + created.ID)
	patientBody, err := getWithToken(baseURL+"/fhir/Patient/"+created.ID, tokenResp.AccessToken)
	if err != nil {
		return err
	}

	fmt.Println("oauth-fhir-server: self-contained SMART auth + FHIR")
	fmt.Printf("base URL: %s\n", baseURL)
	fmt.Printf("unauthenticated GET /fhir/Patient status: %d (expect 401)\n", unauthStatus)
	fmt.Printf("authenticated GET /fhir/Patient/%s\n", created.ID)
	fmt.Println(appkit.PrettyJSON(patientBody))
	return nil
}

func queryValue(rawURL, key string) string {
	idx := stringsIndex(rawURL, "?")
	if idx < 0 {
		return ""
	}
	query := rawURL[idx+1:]
	for query != "" {
		amp := stringsIndex(query, "&")
		part := query
		if amp >= 0 {
			part = query[:amp]
			query = query[amp+1:]
		} else {
			query = ""
		}
		eq := stringsIndex(part, "=")
		if eq < 0 {
			continue
		}
		if part[:eq] == key {
			return part[eq+1:]
		}
	}
	return ""
}

func stringsIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func getWithToken(url, token string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: status %d: %s", url, resp.StatusCode, body)
	}
	return body, nil
}

func head(url string) (int, error) {
	resp, err := http.Head(url)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}
