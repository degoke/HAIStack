package oauth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

func TestSessionUserAuthenticator(t *testing.T) {
	auth, err := oauth.NewSessionUserAuthenticator(oauth.SessionAuthConfig{Secret: "session-secret"})
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
	}
	user, ok := auth.AuthenticateUser(req)
	if !ok || user.Subject != "clinician-1" {
		t.Fatalf("user = %#v ok = %v", user, ok)
	}
}
