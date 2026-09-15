package oauth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// ErrInvalidConfig indicates invalid OAuth server configuration.
var ErrInvalidConfig = errors.New("oauth: invalid config")

func writeMethodNotAllowed(w http.ResponseWriter, allowed ...string) {
	msg := "method not allowed"
	if len(allowed) > 0 {
		msg = "method not allowed; use " + strings.Join(allowed, " or ")
	}
	writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", msg)
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             code,
		"error_description": description,
	})
}
