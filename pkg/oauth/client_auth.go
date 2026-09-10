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

func (s *Server) requireAuthenticatedClient(w http.ResponseWriter, r *http.Request) (Client, bool) {
	client, err := s.authenticateClient(r)
	if err != nil {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "client authentication required")
		return Client{}, false
	}
	return client, true
}
