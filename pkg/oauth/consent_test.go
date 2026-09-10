package oauth_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

func TestServeConsentPageEscapesHTML(t *testing.T) {
	rec := httptest.NewRecorder()
	oauth.ServeConsentPage(rec, oauth.AuthorizationRequest{
		ClientID: "<script>alert(1)</script>",
		Scope:    "patient/<img src=x onerror=alert(1)>.rs",
	}, "csrf-<token>")
	body := rec.Body.String()
	if strings.Contains(body, "<script>") || strings.Contains(body, "<img") {
		t.Fatalf("unescaped html in consent page: %s", body)
	}
	if !strings.Contains(body, `value="csrf-&lt;token&gt;"`) {
		t.Fatalf("csrf token not escaped: %s", body)
	}
}
