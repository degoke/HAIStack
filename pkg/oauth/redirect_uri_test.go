package oauth_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

func TestValidateRedirectURI(t *testing.T) {
	if err := oauth.ValidateRedirectURI("https://app.example/callback"); err != nil {
		t.Fatal(err)
	}
	if err := oauth.ValidateRedirectURI("http://127.0.0.1/callback"); err != nil {
		t.Fatal(err)
	}
	if err := oauth.ValidateRedirectURI("http://app.example/callback"); err == nil {
		t.Fatal("expected non-loopback http redirect to fail")
	}
}
