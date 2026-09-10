package oauth

import (
	"net/http"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed form body")
		return
	}
	token := strings.TrimSpace(r.Form.Get("token"))
	if token == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "token is required")
		return
	}
	if _, ok := s.requireAuthenticatedClient(w, r); !ok {
		return
	}

	hint := strings.TrimSpace(r.Form.Get("token_type_hint"))
	switch hint {
	case "", "refresh_token":
		if s.refresh != nil {
			if err := s.refresh.Revoke(token); err == nil {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
	case "access_token":
		// fall through to access-token handling
	}

	if s.revocation != nil {
		claims, err := smart.ParseTokenUnverified(token)
		if err == nil && claims.JWTID != "" {
			exp := claims.ExpiresAt
			if exp.IsZero() {
				exp = s.now().Add(s.tokenTTL)
			}
			_ = s.revocation.Revoke(claims.JWTID, "access_token", exp)
		}
	}
	if s.refresh != nil {
		_ = s.refresh.Revoke(token)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) RevocationEndpoint() string {
	return s.issuer + "/oauth/revoke"
}

func (s *Server) now() time.Time {
	if s != nil && s.nowFn != nil {
		return s.nowFn()
	}
	return time.Now()
}
