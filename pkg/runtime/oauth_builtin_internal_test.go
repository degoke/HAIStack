package runtime

import "testing"

func TestResolveBuiltinOAuthIssuerHostlessListenAddr(t *testing.T) {
	issuer, err := resolveBuiltinOAuthIssuer("", ":8080")
	if err != nil {
		t.Fatalf("resolve issuer: %v", err)
	}
	if issuer != "http://127.0.0.1:8080" {
		t.Fatalf("issuer = %q", issuer)
	}
}

func TestResolveBuiltinOAuthIssuerExplicitURL(t *testing.T) {
	issuer, err := resolveBuiltinOAuthIssuer("https://auth.example.test/", "")
	if err != nil {
		t.Fatalf("resolve issuer: %v", err)
	}
	if issuer != "https://auth.example.test" {
		t.Fatalf("issuer = %q", issuer)
	}
}
