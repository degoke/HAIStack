package oauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

func generateClientSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return randomToken()
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func clientSecretFromRequest(r *http.Request) string {
	if secret, _, ok := r.BasicAuth(); ok {
		return secret
	}
	return r.Form.Get("client_secret")
}

func validateClientSecret(client Client, provided string) bool {
	if strings.TrimSpace(client.ClientSecret) == "" {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(client.ClientSecret), []byte(provided)) == 1
}
