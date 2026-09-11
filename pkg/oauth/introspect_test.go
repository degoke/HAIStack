package oauth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

func TestTokenIntrospection_ActiveAccessToken(t *testing.T) {
	_, srv, ts := newTestServer(t, testServerOpts{})
	defer ts.Close()
	tokenResp := exchangeAuthCode(t, ts.URL, srv, "patient/Patient.read")

	values := url.Values{}
	values.Set("token", tokenResp.AccessToken)
	values.Set("token_type_hint", "access_token")
	values.Set("client_id", "introspect-client")
	values.Set("client_secret", "introspect-secret")
	resp, err := http.Post(ts.URL+"/oauth/introspect", "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var doc oauth.IntrospectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Active {
		t.Fatalf("doc = %#v", doc)
	}
	if doc.Scope == "" || doc.Iss == "" {
		t.Fatalf("doc = %#v", doc)
	}
}

func TestTokenIntrospection_RevokedInactive(t *testing.T) {
	_, srv, ts := newTestServer(t, testServerOpts{})
	defer ts.Close()
	tokenResp := exchangeAuthCode(t, ts.URL, srv, "patient/Patient.read")
	jti, exp, err := oauth.ParseAccessTokenJTI(tokenResp.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.RevocationStore().Revoke(jti, "access_token", exp); err != nil {
		t.Fatal(err)
	}
	doc := srv.IntrospectToken(tokenResp.AccessToken, "access_token")
	if doc.Active {
		t.Fatalf("doc = %#v", doc)
	}
}

func TestIntrospectionRequiresClientAuth(t *testing.T) {
	_, srv, ts := newTestServer(t, testServerOpts{})
	defer ts.Close()
	tokenResp := exchangeAuthCode(t, ts.URL, srv, "patient/Patient.read")

	values := url.Values{}
	values.Set("token", tokenResp.AccessToken)
	resp, err := http.Post(ts.URL+"/oauth/introspect", "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestMultiTenantIssuerRouting(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	baseClients := oauth.NewClientRegistry()
	_ = baseClients.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "shared-app",
			RedirectURIs: []string{"https://app/cb"},
			Scopes:       []string{"patient/Patient.read", "openid"},
		},
		DefaultPatient: "p1",
		TenantHint:     "tenant-a",
	})

	tenants := oauth.NewTenantRegistry()
	_ = tenants.Register(oauth.TenantIssuerConfig{
		TenantID:    "tenant-a",
		Issuer:      "https://example.com/t/tenant-a",
		FHIRBaseURL: "https://example.com/t/tenant-a/fhir",
	})
	_ = tenants.Register(oauth.TenantIssuerConfig{
		TenantID:    "tenant-b",
		Issuer:      "https://example.com/t/tenant-b",
		FHIRBaseURL: "https://example.com/t/tenant-b/fhir",
	})

	mts, err := oauth.NewMultiTenantServer(oauth.MultiTenantConfig{
		Base: oauth.Config{
			Signer:  oauth.RS256Signer{PrivateKey: key, Kid: "test"},
			Clients: baseClients,
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
	if srvA.Issuer() != "https://example.com/t/tenant-a" {
		t.Fatalf("issuer = %q", srvA.Issuer())
	}

	rec := serveHTTP(mts.Handler(), http.MethodGet, "/t/tenant-a/.well-known/smart-configuration", nil)
	if rec.status != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.status, rec.body)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(rec.body), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["issuer"] != "https://example.com/t/tenant-a" {
		t.Fatalf("doc = %#v", doc)
	}
	if doc["introspection_endpoint"] == "" {
		t.Fatalf("doc = %#v", doc)
	}
}

func TestWireMultiTenantHTTP_ResolvesByIssuer(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	clients := oauth.NewClientRegistry()
	_ = clients.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "app",
			RedirectURIs: []string{"https://app/cb"},
			Scopes:       []string{"patient/Patient.read"},
		},
		DefaultPatient: "p1",
		DefaultUser:    "Practitioner/demo",
		TenantHint:     "tenant-a",
	})
	tenants := oauth.NewTenantRegistry()
	_ = tenants.Register(oauth.TenantIssuerConfig{
		TenantID:    "tenant-a",
		Issuer:      "https://host/t/tenant-a",
		FHIRBaseURL: "https://host/t/tenant-a/fhir",
	})
	token, _, err := oauth.IssueAccessToken(oauth.RS256Signer{PrivateKey: key}, "https://host/t/tenant-a", oauth.AccessTokenClaims{
		Subject:    "user-1",
		ClientID:   "app",
		Scope:      "patient/Patient.read",
		Audience:   "https://host/t/tenant-a/fhir",
		TenantHint: "tenant-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	wired, err := oauth.WireMultiTenantHTTP(oauth.MultiTenantConfig{
		Base: oauth.Config{
			Signer:  oauth.RS256Signer{PrivateKey: key},
			Clients: clients,
		},
		Tenants: tenants,
	}, smart.NewAuthAdapter(smart.AuthAdapterConfig{
		DefaultTenantID:  "tenant-a",
		DefaultUserRoles: []string{"clinician"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "/fhir/Patient/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	_, tenant, err := wired.PrincipalResolver(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if tenant.TenantID != "tenant-a" {
		t.Fatalf("tenant = %#v", tenant)
	}
}

type httpCapture struct {
	status int
	body   string
}

func serveHTTP(handler http.Handler, method, path string, header http.Header) httpCapture {
	req, _ := http.NewRequest(method, path, nil)
	for k, vals := range header {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	rec := captureRecorder{}
	handler.ServeHTTP(&rec, req)
	return httpCapture{status: rec.code, body: rec.body.String()}
}

type captureRecorder struct {
	code int
	body strings.Builder
	hdr  http.Header
}

func (r *captureRecorder) Header() http.Header {
	if r.hdr == nil {
		r.hdr = make(http.Header)
	}
	return r.hdr
}

func (r *captureRecorder) Write(b []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.body.Write(b)
}

func (r *captureRecorder) WriteHeader(statusCode int) {
	r.code = statusCode
}
