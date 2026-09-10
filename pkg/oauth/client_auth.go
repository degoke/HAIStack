package oauth

import (
	"net/http"
	"strings"
)

func (s *Server) authenticateClient(r *http.Request) (Client, error) {
	if err := r.ParseForm(); err != nil {
		return Client{}, err
	}
	clientID := strings.TrimSpace(r.Form.Get("client_id"))
	if clientID == "" {
		return Client{}, ErrInvalidClient
	}
	client, err := s.clients.Lookup(clientID)
	if err != nil {
		return Client{}, err
	}
	if client.Confidential {
		secret := strings.TrimSpace(r.Form.Get("client_secret"))
		if secret == "" || secret != client.ClientSecret {
			return Client{}, ErrInvalidClient
		}
	}
	return client, nil
}

func (s *Server) requireConfidentialClient(w http.ResponseWriter, r *http.Request) (Client, bool) {
	client, err := s.authenticateClient(r)
	if err != nil || !client.Confidential {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "confidential client authentication required")
		return Client{}, false
	}
	return client, true
}

func (s *Server) authenticateLaunchIssuer(r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return err
	}
	if s.cfg.LaunchIssuerAuth == nil {
		return ErrInvalidClient
	}
	id := strings.TrimSpace(r.Form.Get("launch_issuer_id"))
	secret := strings.TrimSpace(r.Form.Get("launch_issuer_secret"))
	if id == "" || secret == "" {
		return ErrInvalidClient
	}
	auth := s.cfg.LaunchIssuerAuth
	if id != auth.ClientID || secret != auth.ClientSecret {
		return ErrInvalidClient
	}
	return nil
}

func (s *Server) requireLaunchIssuer(w http.ResponseWriter, r *http.Request) bool {
	if err := s.authenticateLaunchIssuer(r); err != nil {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "launch issuer authentication required")
		return false
	}
	return true
}
