package oauth

import (
	"net/http"
	"strings"
)

func (s *Server) authenticateClient(r *http.Request) (Client, error) {
	clientID, secret, err := clientCredentialsFromRequest(r)
	if err != nil {
		return Client{}, err
	}
	if clientID == "" {
		return Client{}, ErrInvalidClient
	}
	client, err := s.clients.Lookup(s.issuer, clientID)
	if err != nil {
		return Client{}, err
	}
	if client.Confidential {
		if !verifyClientSecret(client, secret) {
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
	if err := s.validateLaunchIssuerMTLS(r); err != nil {
		return err
	}
	if s.launchAuthenticatedByMTLS(r) {
		return nil
	}
	if err := r.ParseForm(); err != nil {
		return err
	}
	id := strings.TrimSpace(r.Form.Get("launch_issuer_id"))
	secret := strings.TrimSpace(r.Form.Get("launch_issuer_secret"))
	if id == "" || secret == "" {
		return ErrInvalidClient
	}
	if s.cfg.LaunchIssuers != nil && s.cfg.LaunchIssuers.Validate(id, secret) {
		return nil
	}
	if auth := s.cfg.LaunchIssuerAuth; auth != nil {
		if id == s.launchIssuerAuthID && verifyStoredClientSecret(s.launchIssuerAuthHash, secret) {
			return nil
		}
	}
	return ErrInvalidClient
}

func (s *Server) requireLaunchIssuer(w http.ResponseWriter, r *http.Request) bool {
	if err := s.authenticateLaunchIssuer(r); err != nil {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "launch issuer authentication required")
		return false
	}
	return true
}
