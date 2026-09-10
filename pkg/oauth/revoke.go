package oauth

import (
	"net/http"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid form")
		return
	}
	if _, _, err := s.lookupAuthenticatedClient(r, r.Form.Get("client_id")); err != nil {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", err.Error())
		return
	}
	token := strings.TrimSpace(r.Form.Get("token"))
	if token == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "token is required")
		return
	}
	hint := strings.TrimSpace(r.Form.Get("token_type_hint"))
	switch hint {
	case "refresh_token":
		_ = s.authStore.DeleteRefreshToken(token)
	case "access_token":
		s.revokeAccessToken(token)
	default:
		if s.authStore.DeleteRefreshToken(token) {
			break
		}
		s.revokeAccessToken(token)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) revokeAccessToken(token string) {
	if s.revocationStore == nil {
		return
	}
	claims, err := smart.ParseTokenUnverified(token)
	if err != nil || claims.JWTID == "" {
		return
	}
	expiry := claims.ExpiresAt
	if expiry.IsZero() {
		expiry = s.cfg.Now().Add(s.cfg.AccessTokenTTL)
	}
	_ = s.revocationStore.Revoke(claims.JWTID, expiry)
}
