package oauth_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

func TestSigningKeyEncryptionRoundTrip(t *testing.T) {
	secret := "test-signing-key-secret"
	pem := []byte("-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n")
	ciphertext, nonce, err := oauth.EncryptSigningKeyPEM(pem, secret)
	if err != nil {
		t.Fatal(err)
	}
	if ciphertext == string(pem) || nonce == "" {
		t.Fatal("expected encrypted payload")
	}
	plain, err := oauth.DecryptSigningKeyPEM(ciphertext, nonce, secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != string(pem) {
		t.Fatalf("round trip = %q", plain)
	}
}
