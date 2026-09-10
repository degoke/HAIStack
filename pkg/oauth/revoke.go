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
	_, clientID, err := s.lookupAuthenticatedClient(r, r.Form.Get("client_id"))
	if err != nil {
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
		s.revokeRefreshToken(token, clientID)
	case "access_token":
		s.revokeAccessToken(token, clientID)
	default:
		if s.revokeRefreshToken(token, clientID) {
			break
		}
		s.revokeAccessToken(token, clientID)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) revokeRefreshToken(token, clientID string) bool {
	entry, ok := s.authStore.GetRefreshToken(token)
	if !ok || entry.ClientID != clientID {
		return false
	}
	return s.authStore.DeleteRefreshToken(token)
}

func (s *Server) revokeAccessToken(token, clientID string) {
	if s.revocationStore == nil {
		return
	}
	claims, err := smart.ParseTokenUnverified(token)
	if err != nil || claims.JWTID == "" {
		return
	}
	if claims.Issuer != "" && claims.Issuer != s.cfg.Issuer {
		return
	}
	if claims.ClientID != "" && claims.ClientID != clientID {
		return
	}
	expiry := claims.ExpiresAt
	if expiry.IsZero() {
		expiry = s.cfg.Now().Add(s.cfg.AccessTokenTTL)
	}
	_ = s.revocationStore.Revoke(claims.JWTID, expiry)
}
