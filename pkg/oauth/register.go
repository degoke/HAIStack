package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

type clientRegistrationRequest struct {
	ClientID                string   `json:"client_id,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	Scopes                  []string `json:"scopes,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

type clientRegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

func (s *Server) dynamicClientRegistrationEnabled() bool {
	if s == nil {
		return false
	}
	if s.cfg.DynamicClientRegistration != nil {
		return *s.cfg.DynamicClientRegistration
	}
	return true
}

func (s *Server) RegistrationEndpoint() string {
	return s.issuer + "/oauth/register"
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.dynamicClientRegistrationEnabled() {
		writeOAuthError(w, http.StatusNotFound, "not_found", "dynamic client registration is disabled")
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if token := strings.TrimSpace(s.cfg.RegistrationAccessToken); token != "" {
		if !registrationAuthorized(r, token) {
			writeOAuthError(w, http.StatusUnauthorized, "invalid_token", "registration requires authorization")
			return
		}
	}
	var req clientRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "malformed registration request")
		return
	}
	const maxRegisterAttempts = 5
	var client Client
	var secret string
	var registerErr error
	var err error
	for attempt := 0; attempt < maxRegisterAttempts; attempt++ {
		client, secret, err = s.buildRegisteredClient(req)
		if err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", err.Error())
			return
		}
		registerErr = s.clients.Register(s.issuer, client)
		if registerErr == nil {
			break
		}
		if !errors.Is(registerErr, ErrClientExists) {
			break
		}
	}
	if registerErr != nil {
		if errors.Is(registerErr, ErrClientExists) {
			writeOAuthError(w, http.StatusConflict, "invalid_client_metadata", "client_id already registered")
			return
		}
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", registerErr.Error())
		return
	}
	issuedAt := s.nowFn().Unix()
	resp := clientRegistrationResponse{
		ClientID:                client.ClientID,
		ClientSecret:            secret,
		ClientIDIssuedAt:        issuedAt,
		ClientSecretExpiresAt:   0,
		ClientName:              client.ClientName,
		RedirectURIs:            append([]string(nil), client.RedirectURIs...),
		GrantTypes:              append([]string(nil), client.GrantTypes...),
		ResponseTypes:           append([]string(nil), client.ResponseTypes...),
		Scope:                   strings.Join(client.Scopes, " "),
		TokenEndpointAuthMethod: client.TokenEndpointAuthMethod,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) buildRegisteredClient(req clientRegistrationRequest) (Client, string, error) {
	if len(req.RedirectURIs) == 0 {
		return Client{}, "", ErrInvalidConfig
	}
	if strings.TrimSpace(req.ClientID) != "" {
		return Client{}, "", fmt.Errorf("%w: client_id must not be supplied", ErrInvalidConfig)
	}
	clientID, err := NewRandomToken()
	if err != nil {
		return Client{}, "", err
	}
	authMethod := strings.ToLower(strings.TrimSpace(req.TokenEndpointAuthMethod))
	confidential := authMethod == "" || authMethod == "client_secret_basic" || authMethod == "client_secret_post"
	if authMethod == "none" {
		confidential = false
	}
	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code", "refresh_token"}
	}
	responseTypes := req.ResponseTypes
	if len(responseTypes) == 0 {
		responseTypes = []string{"code"}
	}
	scopes := req.Scopes
	if len(scopes) == 0 && strings.TrimSpace(req.Scope) != "" {
		scopes = strings.Fields(req.Scope)
	}
	if authMethod == "" {
		if confidential {
			authMethod = "client_secret_basic"
		} else {
			authMethod = "none"
		}
	}
	secret := ""
	if confidential {
		var err error
		secret, err = NewRandomToken()
		if err != nil {
			return Client{}, "", err
		}
	}
	client := Client{
		ClientRegistration: smartClientRegistration(clientID, req.ClientName, req.RedirectURIs, grantTypes, responseTypes, scopes, authMethod),
		Confidential:       confidential,
		ClientSecret:       secret,
	}
	if err := validateClientRegistration(client); err != nil {
		return Client{}, "", err
	}
	return client, secret, nil
}

func smartClientRegistration(clientID, clientName string, redirectURIs, grantTypes, responseTypes, scopes []string, authMethod string) smart.ClientRegistration {
	return smart.ClientRegistration{
		ClientID:                clientID,
		ClientName:              clientName,
		RedirectURIs:            redirectURIs,
		GrantTypes:              grantTypes,
		ResponseTypes:           responseTypes,
		Scopes:                  scopes,
		TokenEndpointAuthMethod: authMethod,
	}
}
