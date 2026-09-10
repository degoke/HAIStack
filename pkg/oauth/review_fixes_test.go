package oauth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/client"
	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

type loggedInConsentLogin struct {
	subject string
}

func (l loggedInConsentLogin) EnsureLoggedIn(w http.ResponseWriter, _ *http.Request, _ string) (string, bool) {
	return l.subject, true
}

func TestConsentLoginSubjectInToken(t *testing.T) {
	autoApprove := false
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	reg := oauth.NewClientRegistry()
	_ = reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "standalone-app",
			RedirectURIs: []string{"https://app.example/callback"},
			Scopes:       []string{"patient/Patient.read", "openid"},
		},
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	issuer := "http://" + listener.Addr().String()
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:       issuer,
		FHIRBaseURL:  issuer + "/fhir",
		Signer:       oauth.RS256Signer{PrivateKey: key, Kid: "test"},
		Clients:      reg,
		AutoApprove:  &autoApprove,
		ConsentLogin: loggedInConsentLogin{subject: "Practitioner/consent-user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.Listener = listener
	ts.Start()
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	httpClient := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	pkce, err := client.NewPKCEChallenge()
	if err != nil {
		t.Fatal(err)
	}
	authURL := ts.URL + "/oauth/authorize?response_type=code&client_id=standalone-app&redirect_uri=https://app.example/callback&scope=patient/Patient.read%20openid&code_challenge=" + pkce.Challenge + "&code_challenge_method=S256"
	resp, err := httpClient.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	csrf := extractInputValue(string(body), "csrf_token")
	values := url.Values{}
	values.Set("csrf_token", csrf)
	values.Set("approved", "yes")
	resp, err = httpClient.Post(authURL, "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	loc := resp.Header.Get("Location")
	code := parseQueryParam(loc, "code")
	smartClient, err := client.New(client.Config{BaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := smartClient.SMART().Discover(context.Background(), ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	tokenResp, err := smartClient.SMART().ExchangeAuthCode(context.Background(), cfg.TokenEndpoint, "standalone-app", "https://app.example/callback", code, pkce)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := smart.ParseTokenUnverified(tokenResp.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "Practitioner/consent-user" {
		t.Fatalf("subject = %q", claims.Subject)
	}
}

func TestMultiTenantLaunchDoesNotInheritBaseCredentials(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tenants := oauth.NewTenantRegistry()
	_ = tenants.Register(oauth.TenantIssuerConfig{
		TenantID:    "tenant-a",
		Issuer:      "http://example.test/t/tenant-a",
		FHIRBaseURL: "http://example.test/t/tenant-a/fhir",
	})
	mts, err := oauth.NewMultiTenantServer(oauth.MultiTenantConfig{
		Base: oauth.Config{
			Signer:  oauth.RS256Signer{PrivateKey: key, Kid: "test"},
			Clients: oauth.NewClientRegistry(),
			LaunchIssuerAuth: &oauth.LaunchIssuerAuth{
				ClientID:     "ehr-launcher",
				ClientSecret: "ehr-secret",
			},
		},
		Tenants: tenants,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mts.Handler())
	defer ts.Close()
	values := url.Values{}
	values.Set("launch_issuer_id", "ehr-launcher")
	values.Set("launch_issuer_secret", "ehr-secret")
	values.Set("patient", "p1")
	resp, err := http.Post(ts.URL+"/t/tenant-a/oauth/launch", "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, expected tenant without launch creds to reject base credentials", resp.StatusCode)
	}
}

func TestAuthCodeIssuerMismatchRejected(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	reg := oauth.NewClientRegistry()
	_ = reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "app",
			RedirectURIs: []string{"https://app/cb"},
			Scopes:       []string{"patient/Patient.read"},
		},
	})
	codes := oauth.NewCodeStore()
	autoApprove := true
	tenants := oauth.NewTenantRegistry()
	_ = tenants.Register(oauth.TenantIssuerConfig{
		TenantID:    "tenant-a",
		Issuer:      "http://example.test/t/tenant-a",
		FHIRBaseURL: "http://example.test/t/tenant-a/fhir",
		AutoApprove: &autoApprove,
	})
	_ = tenants.Register(oauth.TenantIssuerConfig{
		TenantID:    "tenant-b",
		Issuer:      "http://example.test/t/tenant-b",
		FHIRBaseURL: "http://example.test/t/tenant-b/fhir",
		AutoApprove: &autoApprove,
	})
	mts, err := oauth.NewMultiTenantServer(oauth.MultiTenantConfig{
		Base: oauth.Config{
			Signer:    oauth.RS256Signer{PrivateKey: key, Kid: "test"},
			Clients:   reg,
			CodeStore: codes,
		},
		Tenants: tenants,
	})
	if err != nil {
		t.Fatal(err)
	}
	srvA, err := mts.ServerForTenant("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	code, err := codes.Issue(oauth.AuthCode{
		ClientID:    "app",
		RedirectURI: "https://app/cb",
		Scope:       "patient/Patient.read",
		Issuer:      srvA.Issuer(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mts.Handler())
	defer ts.Close()
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("client_id", "app")
	values.Set("redirect_uri", "https://app/cb")
	resp, err := http.PostForm(ts.URL+"/t/tenant-b/oauth/token", values)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body = %s", resp.StatusCode, body)
	}
}

func TestConsentLogoURLRejectsUnsafeScheme(t *testing.T) {
	autoApprove := false
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	reg := oauth.NewClientRegistry()
	_ = reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "standalone-app",
			RedirectURIs: []string{"https://app.example/callback"},
			Scopes:       []string{"patient/Patient.read"},
		},
	})
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:      "http://example.test",
		FHIRBaseURL: "http://example.test/fhir",
		Signer:      oauth.RS256Signer{PrivateKey: key, Kid: "test"},
		Clients:     reg,
		AutoApprove: &autoApprove,
		ConsentUI: oauth.ConsentUIConfig{
			LogoURL: "javascript:alert(1)",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/oauth/authorize?response_type=code&client_id=standalone-app&redirect_uri=https://app.example/callback&scope=patient/Patient.read&code_challenge=abc&code_challenge_method=S256")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "javascript:") {
		t.Fatalf("unsafe logo URL rendered: %s", body)
	}
}

func parseQueryParam(rawURL, key string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}
