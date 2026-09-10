package oauth

import (
	"net/http"
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
	clientID := r.Form.Get("client_id")
	client, ok := s.cfg.Clients.Get(clientID)
	if !ok {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "unknown client")
		return
	}
	creds := clientCredentialsFromRequest(r, clientID)
	if !authenticateConfidentialClient(client, creds) {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "client authentication failed")
		return
	}
	token := r.Form.Get("token")
	if token == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "token is required")
		return
	}
	_ = s.authStore.DeleteRefreshToken(token)
	w.WriteHeader(http.StatusOK)
}
