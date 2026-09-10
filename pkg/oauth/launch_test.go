package oauth_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/client"
	"github.com/degoke/health-ai-stack/pkg/oauth"
)

func TestOAuthServer_LaunchContextAndUI(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := strings.TrimSuffix(srv.URL, "/")

	server, err := oauth.NewServer(oauth.Config{
		Issuer:         base,
		FHIRAudience:   base,
		AutoApprove:    true,
		LaunchResolver: oauth.StaticLaunchResolver(oauth.LaunchContext{
			PatientID: "pat-launch-1",
			Encounter: "enc-launch-1",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterClient(oauth.Client{
		ClientID:     "launch-client",
		RedirectURIs: []string{"https://localhost/callback"},
		Scopes:       []string{"patient/Patient.rs launch/patient"},
	})
	mux.Handle("/", server.Handler())

	launchResp, err := http.Get(base + "/oauth/launch?launch=tok-1&iss=" + base)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(launchResp.Body)
	_ = launchResp.Body.Close()
	if launchResp.StatusCode != http.StatusOK {
		t.Fatalf("launch status=%d body=%s", launchResp.StatusCode, body)
	}
	var launchJSON map[string]any
	if err := json.Unmarshal(body, &launchJSON); err != nil {
		t.Fatal(err)
	}
	if launchJSON["patient"] != "pat-launch-1" {
		t.Fatalf("launch context = %v", launchJSON)
	}

	pkce, err := client.NewPKCEChallenge()
	if err != nil {
		t.Fatal(err)
	}
	uiResp, err := http.Get(base + "/oauth/launch/ui?launch=tok-1&iss=" + base +
		"&client_id=launch-client&redirect_uri=https://localhost/callback" +
		"&code_challenge=" + pkce.Challenge + "&code_challenge_method=S256")
	if err != nil {
		t.Fatal(err)
	}
	uiBody, _ := io.ReadAll(uiResp.Body)
	_ = uiResp.Body.Close()
	if uiResp.StatusCode != http.StatusOK || !strings.Contains(string(uiBody), "SMART EHR Launch") {
		t.Fatalf("launch ui status=%d", uiResp.StatusCode)
	}
	if !strings.Contains(string(uiBody), pkce.Challenge) {
		t.Fatalf("launch ui missing pkce challenge in authorize link")
	}

	httpClient, _ := client.New(client.Config{BaseURL: base})
	cfg, _ := httpClient.SMART().Discover(context.Background(), base)
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "launch-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs launch/patient", Launch: "tok-1", Aud: base, PKCE: pkce,
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
		t.Fatalf("authorize status=%d", authResp.StatusCode)
	}
	loc := authResp.Header.Get("Location")
	code := strings.Split(strings.Split(loc, "code=")[1], "&")[0]
	tokenResp, err := httpClient.SMART().ExchangeAuthCode(context.Background(), client.AuthCodeExchangeRequest{
		TokenEndpoint: cfg.TokenEndpoint,
		ClientID:      "launch-client",
		RedirectURI:   "https://localhost/callback",
		Code:          code,
		PKCE:          pkce,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tokenResp.Patient != "pat-launch-1" {
		t.Fatalf("patient claim = %q", tokenResp.Patient)
	}
	if tokenResp.Encounter != "enc-launch-1" {
		t.Fatalf("encounter claim = %q", tokenResp.Encounter)
	}
}
