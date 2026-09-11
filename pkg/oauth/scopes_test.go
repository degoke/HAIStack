package oauth_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

func TestNormalizeRegisteredClientScopesDefaults(t *testing.T) {
	t.Parallel()
	scopes, err := oauth.NormalizeRegisteredClientScopes(oauth.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) == 0 {
		t.Fatal("expected default scopes")
	}
}

func TestNormalizeRegisteredClientScopesRejectsDisallowed(t *testing.T) {
	t.Parallel()
	_, err := oauth.NormalizeRegisteredClientScopes(oauth.Config{}, []string{"system/*.read"})
	if err == nil {
		t.Fatal("expected disallowed scope to fail")
	}
}

func TestNormalizeRegisteredClientScopesAllowsSubset(t *testing.T) {
	t.Parallel()
	scopes, err := oauth.NormalizeRegisteredClientScopes(oauth.Config{}, []string{"openid", "patient/*.read"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 2 {
		t.Fatalf("scopes = %#v", scopes)
	}
}
