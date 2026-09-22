package oauth_test

import (
	"testing"

	"github.com/degoke/haistack/pkg/oauth"
)

func TestEncryptDecryptSigningKeyPEM(t *testing.T) {
	pem := []byte("-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----")
	ciphertext, nonce, err := oauth.EncryptSigningKeyPEM(pem, "encryption-secret")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := oauth.DecryptSigningKeyPEM(ciphertext, nonce, "encryption-secret")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(pem) {
		t.Fatalf("round trip failed: %q", raw)
	}
}
