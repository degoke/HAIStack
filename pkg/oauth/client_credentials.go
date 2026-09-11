package oauth

import (
	"net/http"
	"strings"
)

func clientCredentialsFromRequest(r *http.Request) (clientID, clientSecret string, err error) {
	if err := r.ParseForm(); err != nil {
		return "", "", err
	}
	if user, pass, ok := r.BasicAuth(); ok {
		return strings.TrimSpace(user), pass, nil
	}
	return strings.TrimSpace(r.Form.Get("client_id")), strings.TrimSpace(r.Form.Get("client_secret")), nil
}

func registrationAuthorized(r *http.Request, expectedToken string) bool {
	expectedToken = strings.TrimSpace(expectedToken)
	if expectedToken == "" {
		return true
	}
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	return tokenEqual(got, expectedToken)
}
