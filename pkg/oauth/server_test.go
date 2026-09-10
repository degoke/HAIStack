package oauth_test

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
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/client"
	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

func TestOAuthServer_DiscoveryAndPKCEFlow(t *testing.T) {
	mux := http.NewServeMux()
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	base := strings.TrimSuffix(httpServer.URL, "/")
	server, err := oauth.NewServer(oauth.Config{Issuer: base, FHIRAudience: base, AutoApprove: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = server.RegisterClient(oauth.Client{
		ClientID:     "demo-client",
		RedirectURIs: []string{"https://app.example/callback"},
		Scopes:       []string{"patient/Patient.rs"},
	})
	mux.Handle("/", server.Handler())
	httpClient, err := client.New(client.Config{BaseURL: base})
	if err != nil {
		t.Fatal(err)
	}
	smartClient := httpClient.SMART()
	cfg, err := smartClient.Discover(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TokenEndpoint == "" || cfg.AuthorizationEndpoint == "" {
		t.Fatalf("discovery = %+v", cfg)
	}

	pkce, err := client.NewPKCEChallenge()
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := smartClient.BuildAuthURL(client.AuthCodeRequest{
		Config:      cfg,
		ClientID:      "demo-client",
		RedirectURI: "https://app.example/callback",
		Scope:       "patient/Patient.rs",
		State:       "state-1",
		PKCE:        pkce,
	})
	if err != nil {
		t.Fatal(err)
	}
	clientNoRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authResp, err := clientNoRedirect.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = authResp.Body.Close()
	if authResp.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d", authResp.StatusCode)
	}
	redirectURL, err := url.Parse(authResp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := redirectURL.Query().Get("code")
	if code == "" {
		t.Fatal("missing authorization code")
	}

	tokenResp, err := smartClient.ExchangeAuthCode(context.Background(), client.AuthCodeExchangeRequest{
		TokenEndpoint: cfg.TokenEndpoint,
		ClientID:      "demo-client",
		RedirectURI:   "https://app.example/callback",
		Code:          code,
		PKCE:          pkce,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tokenResp.AccessToken == "" {
		t.Fatal("missing access token")
	}

	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{DefaultTenantID: "t1"})
	bearer := server.BearerAuthConfig(adapter)
	claims, err := bearer.Validator.ValidateToken(tokenResp.AccessToken, bearer.Options)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Scope == "" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestOAuthServer_ClientCredentialsAssertion(t *testing.T) {
	key, pemPub := mustRSAKey(t)
	mux := http.NewServeMux()
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()
	base := strings.TrimSuffix(httpServer.URL, "/")
	now := time.Now()
	server, err := oauth.NewServer(oauth.Config{
		Issuer:       base,
		FHIRAudience: base,
		Now:          func() time.Time { return now },
		AutoApprove:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = server.RegisterClient(oauth.Client{
		ClientID:     "backend-client",
		PublicKeyPEM: pemPub,
		Algorithm:    "RS256",
		Scopes:       []string{"system/Patient.rs"},
		GrantTypes:   []string{"client_credentials"},
	})
	mux.Handle("/", server.Handler())

	assertion := signedAssertionJWT(t, key, map[string]any{
		"iss": "backend-client", "sub": "backend-client", "aud": base + "/oauth/token",
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "jti": "assertion-1",
		"scope": "system/Patient.rs",
	})
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
	form.Set("client_assertion", assertion)
	form.Set("scope", "system/Patient.rs")
	resp, err := http.PostForm(base+"/oauth/token", form)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var tokenResp map[string]any
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		t.Fatal(err)
	}
	if tokenResp["access_token"] == "" {
		t.Fatalf("token response = %s", body)
	}
}

func TestOAuthServer_JWKSAndRegistration(t *testing.T) {
	server, err := oauth.NewServer(oauth.Config{Issuer: "https://issuer.example", AllowDynamicRegistration: true})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", server.Handler())
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/oauth/jwks")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var jwks map[string]any
	if err := json.Unmarshal(body, &jwks); err != nil {
		t.Fatal(err)
	}
	keys, _ := jwks["keys"].([]any)
	if len(keys) != 1 {
		t.Fatalf("jwks = %s", body)
	}

	regResp, err := http.Post(httpServer.URL+"/oauth/register", "application/json", strings.NewReader(`{
		"redirect_uris":["https://app.example/callback"],
		"scope":"patient/*.rs"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if regResp.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d", regResp.StatusCode)
	}
}

func signedAssertionJWT(t *testing.T, key *rsa.PrivateKey, payload map[string]any) string {
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
