package oauth

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

func (s *Server) authenticateTokenRequest(r *http.Request, client Client, formClientID string) (string, error) {
	method := normalizeAuthMethod(client.TokenEndpointAuthMethod)
	if method == AuthMethodPrivateKeyJWT {
		return s.authenticateClientAssertion(r, client)
	}
	if isPublicClient(client) {
		clientID := strings.TrimSpace(formClientID)
		if clientID == "" {
			clientID = client.ClientID
		}
		if clientID != client.ClientID {
			return "", fmt.Errorf("invalid client")
		}
		return clientID, nil
	}
	creds := clientCredentialsFromRequest(r, formClientID)
	clientID := creds.ClientID
	if clientID == "" {
		clientID = strings.TrimSpace(formClientID)
	}
	if clientID == "" {
		clientID = client.ClientID
	}
	if clientID != client.ClientID {
		return "", fmt.Errorf("invalid client")
	}
	if !authenticateConfidentialClient(client, creds) {
		return "", fmt.Errorf("client authentication failed")
	}
	return clientID, nil
}

func (s *Server) authenticateClientAssertion(r *http.Request, client Client) (string, error) {
	assertion := strings.TrimSpace(r.Form.Get("client_assertion"))
	if assertion == "" {
		return "", fmt.Errorf("client_assertion required")
	}
	assertionType := strings.TrimSpace(r.Form.Get("client_assertion_type"))
	if assertionType != "" && assertionType != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
		return "", fmt.Errorf("invalid client_assertion_type")
	}
	claims, backendClient, err := s.backendAuth.ValidateBackendAssertion(assertion, smart.TokenValidateOptions{
		RequireIssuer:   true,
		RequireAudience: true,
		RequireExpiry:   true,
		RequireSubject:  true,
		RequireJWTID:    true,
		Now:             s.cfg.Now,
	})
	if err != nil {
		return "", err
	}
	if backendClient.ClientID != client.ClientID {
		return "", fmt.Errorf("client mismatch")
	}
	_ = claims
	return client.ClientID, nil
}

func (s *Server) lookupAuthenticatedClient(r *http.Request, formClientID string) (Client, string, error) {
	creds := clientCredentialsFromRequest(r, formClientID)
	lookupID := creds.ClientID
	if lookupID == "" {
		lookupID = strings.TrimSpace(formClientID)
	}
	if lookupID == "" && strings.TrimSpace(r.Form.Get("client_assertion")) != "" {
		unverified, err := smart.ParseTokenUnverified(r.Form.Get("client_assertion"))
		if err == nil {
			lookupID = strings.TrimSpace(unverified.ClientID)
			if lookupID == "" {
				lookupID = strings.TrimSpace(unverified.Issuer)
			}
			if lookupID == "" {
				lookupID = strings.TrimSpace(unverified.Subject)
			}
		}
	}
	client, ok := s.cfg.Clients.Get(lookupID)
	if !ok {
		return Client{}, "", fmt.Errorf("unknown client")
	}
	clientID, err := s.authenticateTokenRequest(r, client, formClientID)
	if err != nil {
		return Client{}, "", err
	}
	return client, clientID, nil
}

func isPublicClient(client Client) bool {
	return strings.TrimSpace(client.ClientSecretHash) == "" &&
		strings.TrimSpace(client.ClientSecret) == "" &&
		strings.TrimSpace(client.PublicKeyPEM) == ""
}
