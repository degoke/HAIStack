package oauth_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

func TestSessionUserAuthenticator(t *testing.T) {
	auth, err := oauth.NewSessionUserAuthenticator(oauth.SessionAuthConfig{
		Secret: "session-secret",
		Issuer: "https://auth.example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := auth.SetSession(rec, "clinician-1"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
		if c.Path != "/" {
			t.Fatalf("cookie path = %q", c.Path)
		}
	}
	user, ok := auth.AuthenticateUser(req)
	if !ok || user.Subject != "clinician-1" {
		t.Fatalf("user = %#v ok = %v", user, ok)
	}

	other := auth.ForIssuer("https://auth.example.test/t/other", "/t/other/")
	if _, ok := other.AuthenticateUser(req); ok {
		t.Fatal("expected issuer-bound session to be rejected by another tenant")
	}
}

func TestSessionLoginRequiresPasswordAndAllowlistsReturn(t *testing.T) {
	users, err := oauth.ParsePasswordUsers("clinician-1:s3cret")
	if err != nil || users == nil {
		t.Fatalf("parse users: %v %#v", err, users)
	}
	auth, err := oauth.NewSessionUserAuthenticator(oauth.SessionAuthConfig{
		Secret: "session-secret",
		Issuer: "https://auth.example.test",
		Users:  users,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:            "https://auth.example.test",
		UserAuthenticator: auth,
		LoginPath:         "/oauth/login",
		AutoApprove:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler()

	rec := httptest.NewRecorder()
	form := url.Values{
		"username": {"clinician-1"},
		"return":   {"https://evil.example/phish"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("open redirect status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	form = url.Values{"username": {"clinician-1"}}
	req = httptest.NewRequest(http.MethodPost, "/oauth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("passwordless status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	form = url.Values{
		"username": {"clinician-1"},
		"password": {"wrong"},
	}
	req = httptest.NewRequest(http.MethodPost, "/oauth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad password status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	form = url.Values{
		"username": {"clinician-1"},
		"password": {"s3cret"},
		"return":   {"https://auth.example.test/oauth/authorize?client_id=app"},
	}
	req = httptest.NewRequest(http.MethodPost, "/oauth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("login status = %d body = %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "https://auth.example.test/oauth/authorize?client_id=app" {
		t.Fatalf("location = %q", loc)
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("expected session cookie")
	}
}

func TestSessionLoginRateLimit(t *testing.T) {
	users, err := oauth.ParsePasswordUsers("clinician-1:s3cret")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := oauth.NewSessionUserAuthenticator(oauth.SessionAuthConfig{
		Secret: "session-secret",
		Issuer: "https://auth.example.test",
		Users:  users,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:            "https://auth.example.test",
		UserAuthenticator: auth,
		LoginPath:         "/oauth/login",
		AutoApprove:       true,
		RateLimit:         oauth.RateLimitConfig{LoginRequests: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler()

	post := func(username string) *httptest.ResponseRecorder {
		t.Helper()
		form := url.Values{
			"username": {username},
			"password": {"nope"},
		}
		req := httptest.NewRequest(http.MethodPost, "/oauth/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "203.0.113.10:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := post("missing"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("first login status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := post("missing"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("same-user rate limited status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := post("other"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("different username should not share per-user bucket, status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSessionLoginIPRateLimit(t *testing.T) {
	users, err := oauth.ParsePasswordUsers("clinician-1:s3cret")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := oauth.NewSessionUserAuthenticator(oauth.SessionAuthConfig{
		Secret: "session-secret",
		Issuer: "https://auth.example.test",
		Users:  users,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:            "https://auth.example.test",
		UserAuthenticator: auth,
		LoginPath:         "/oauth/login",
		AutoApprove:       true,
		RateLimit:         oauth.RateLimitConfig{LoginRequests: 100, LoginIPRequests: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler()
	post := func(username string) *httptest.ResponseRecorder {
		t.Helper()
		form := url.Values{"username": {username}, "password": {"nope"}}
		req := httptest.NewRequest(http.MethodPost, "/oauth/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "203.0.113.25:9"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := post("alice"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("first IP login status = %d", rec.Code)
	}
	if rec := post("bob"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("IP spray status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuthorizeLoginRedirectUsesIssuerPath(t *testing.T) {
	users, err := oauth.ParsePasswordUsers("clinician-1:s3cret")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := oauth.NewSessionUserAuthenticator(oauth.SessionAuthConfig{
		Secret: "session-secret",
		Issuer: "https://auth.example.test",
		Users:  users,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:            "https://auth.example.test",
		UserAuthenticator: auth,
		LoginPath:         "/oauth/login",
		AutoApprove:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.RegisterClient(oauth.Client{
		ClientID:     "app",
		ClientSecret: "secret",
		RedirectURIs: []string{"https://app.example/cb"},
		Scopes:       []string{"openid"},
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=app&redirect_uri=https://app.example/cb&response_type=code&scope=openid", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/oauth/login?") {
		t.Fatalf("base login Location = %q", loc)
	}
}

func TestParsePasswordUsers(t *testing.T) {
	users, err := oauth.ParsePasswordUsers("alice:pw1;bob:pw2")
	if err != nil {
		t.Fatal(err)
	}
	if ident, ok := users.Verify("alice", "pw1"); !ok || ident.Subject != "alice" {
		t.Fatalf("alice = %#v ok=%v", ident, ok)
	}
	if _, ok := users.Verify("alice", "wrong"); ok {
		t.Fatal("expected wrong password to fail")
	}
	if _, ok := users.Verify("missing", "pw1"); ok {
		t.Fatal("expected missing user to fail")
	}
}
