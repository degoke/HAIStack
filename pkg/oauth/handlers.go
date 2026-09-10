package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

func (s *Server) handleOpenIDConfiguration(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.openIDConfiguration())
}

func (s *Server) handleSMARTConfiguration(w http.ResponseWriter, _ *http.Request) {
	cfg := smart.DefaultConfiguration(s.cfg.Issuer)
	cfg.JWKSURI = s.cfg.Issuer + "/oauth/jwks"
	cfg.AuthorizationEndpoint = s.cfg.Issuer + "/oauth/authorize"
	cfg.TokenEndpoint = s.cfg.Issuer + "/oauth/token"
	cfg.RegistrationEndpoint = s.cfg.Issuer + "/oauth/register"
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg.SigningKey.JWKS())
}

func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	if clientID == "" || redirectURI == "" {
		http.Error(w, "client_id and redirect_uri are required", http.StatusBadRequest)
		return
	}
	client, ok := s.cfg.Clients.Get(clientID)
	if !ok || !redirectAllowed(client.RedirectURIs, redirectURI) {
		http.Error(w, "invalid client or redirect_uri", http.StatusBadRequest)
		return
	}
	if q.Get("response_type") != "code" {
		http.Error(w, "unsupported response_type", http.StatusBadRequest)
		return
	}
	if challenge := q.Get("code_challenge"); challenge == "" && client.PublicKeyPEM == "" && client.ClientSecret == "" {
		http.Error(w, "code_challenge required for public clients", http.StatusBadRequest)
		return
	}
	authReq := AuthorizationRequest{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		Scope:               q.Get("scope"),
		State:               q.Get("state"),
		Patient:             q.Get("patient"),
		Launch:              q.Get("launch"),
		CodeChallenge:       q.Get("code_challenge"),
		CodeChallengeMethod: q.Get("code_challenge_method"),
	}
	patient, err := s.resolveLaunchPatient(r.Context(), authReq.Launch, q.Get("aud"), authReq.Patient)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	authReq.Patient = patient
	if err := validateRequestedScopes(client, authReq.Scope); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.cfg.RequireConsentForm && s.cfg.ConsentHandler == nil && !s.cfg.AutoApprove {
		id := randomToken()
		_ = s.authStore.SavePendingAuthorization(id, PendingAuthorization{
			Request:   authReq,
			ExpiresAt: s.cfg.Now().Add(s.cfg.AuthCodeTTL),
		})
		http.Redirect(w, r, s.cfg.Issuer+"/oauth/consent?id="+url.QueryEscape(id), http.StatusFound)
		return
	}
	approved, err := s.resolveConsent(r.Context(), authReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !approved {
		s.redirectAuthorizeError(w, r, redirectURI, "access_denied", authReq.State)
		return
	}
	s.issueAuthorizationRedirect(w, r, clientID, authReq)
}

func (s *Server) handleConsent(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing consent id", http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodGet {
		pending, ok := s.authStore.GetPendingAuthorization(id)
		if !ok {
			http.Error(w, "consent session expired", http.StatusBadRequest)
			return
		}
		ServeConsentPage(w, pending.Request)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pending, ok := s.authStore.ConsumePendingAuthorization(id)
	if !ok {
		http.Error(w, "consent session expired", http.StatusBadRequest)
		return
	}
	approved := r.FormValue("approve") == "yes"
	if !approved {
		s.redirectAuthorizeError(w, r, pending.Request.RedirectURI, "access_denied", pending.Request.State)
		return
	}
	s.issueAuthorizationRedirect(w, r, pending.Request.ClientID, pending.Request)
}

func (s *Server) resolveConsent(ctx context.Context, req AuthorizationRequest) (bool, error) {
	switch {
	case s.cfg.ConsentHandler != nil:
		return s.cfg.ConsentHandler.Approve(ctx, req)
	case s.cfg.AutoApprove:
		return true, nil
	default:
		return false, fmt.Errorf("consent handler required; set ConsentHandler or AutoApprove for development")
	}
}

func (s *Server) issueAuthorizationRedirect(w http.ResponseWriter, r *http.Request, clientID string, req AuthorizationRequest) {
	code := randomToken()
	_ = s.authStore.SaveAuthorizationCode(code, AuthorizationCode{
		ClientID:    clientID,
		RedirectURI: req.RedirectURI,
		Scope:       req.Scope,
		Patient:     req.Patient,
		Challenge:   req.CodeChallenge,
		Method:      req.CodeChallengeMethod,
		ExpiresAt:   s.cfg.Now().Add(s.cfg.AuthCodeTTL),
	})
	redirect, err := url.Parse(req.RedirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	params := redirect.Query()
	params.Set("code", code)
	if req.State != "" {
		params.Set("state", req.State)
	}
	redirect.RawQuery = params.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (s *Server) redirectAuthorizeError(w http.ResponseWriter, r *http.Request, redirectURI, errCode, state string) {
	redirect, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	params := redirect.Query()
	params.Set("error", errCode)
	if state != "" {
		params.Set("state", state)
	}
	redirect.RawQuery = params.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		s.handleAuthorizationCode(w, r)
	case "client_credentials":
		s.handleClientCredentials(w, r)
	case "refresh_token":
		s.handleRefreshToken(w, r)
	default:
		http.Error(w, "unsupported grant_type", http.StatusBadRequest)
	}
}

func (s *Server) handleAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	code := r.Form.Get("code")
	redirectURI := r.Form.Get("redirect_uri")
	clientID := r.Form.Get("client_id")
	client, ok := s.cfg.Clients.Get(clientID)
	if !ok {
		http.Error(w, "invalid_client", http.StatusUnauthorized)
		return
	}
	if !validateClientSecret(client, clientSecretFromRequest(r)) {
		http.Error(w, "invalid_client", http.StatusUnauthorized)
		return
	}
	entry, ok := s.authStore.ConsumeAuthorizationCode(code)
	if !ok || entry.ClientID != clientID || entry.RedirectURI != redirectURI {
		http.Error(w, "invalid_grant", http.StatusBadRequest)
		return
	}
	if entry.Challenge != "" {
		verifier := r.Form.Get("code_verifier")
		if !pkceValid(entry.Challenge, entry.Method, verifier) {
			http.Error(w, "invalid_grant", http.StatusBadRequest)
			return
		}
	}
	if err := validateRequestedScopes(client, entry.Scope); err != nil {
		http.Error(w, "invalid_scope", http.StatusBadRequest)
		return
	}
	resp, err := s.issueTokens(clientID, entry.Scope, entry.Patient, clientID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleClientCredentials(w http.ResponseWriter, r *http.Request) {
	assertion := r.Form.Get("client_assertion")
	if assertion == "" {
		http.Error(w, "client_assertion required", http.StatusBadRequest)
		return
	}
	claims, client, err := s.backendAuth.ValidateBackendAssertion(assertion, smart.TokenValidateOptions{
		RequireIssuer:   true,
		RequireAudience: true,
		RequireExpiry:   true,
		RequireSubject:  true,
		RequireJWTID:    true,
		Now:             s.cfg.Now,
	})
	if err != nil {
		http.Error(w, "invalid_client", http.StatusUnauthorized)
		return
	}
	scope := r.Form.Get("scope")
	if scope == "" {
		scope = claims.Scope
	}
	if err := validateRequestedScopes(clientStoreClient(client), scope); err != nil {
		http.Error(w, "invalid_scope", http.StatusBadRequest)
		return
	}
	resp, err := s.issueTokens(claims.ClientID, scope, claims.Patient, claims.Subject)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func clientStoreClient(c smart.BackendClient) Client {
	return Client{ClientID: c.ClientID, Scopes: c.AllowedScopes}
}

func (s *Server) handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	token := r.Form.Get("refresh_token")
	clientID := r.Form.Get("client_id")
	client, ok := s.cfg.Clients.Get(clientID)
	if !ok {
		http.Error(w, "invalid_client", http.StatusUnauthorized)
		return
	}
	if !validateClientSecret(client, clientSecretFromRequest(r)) {
		http.Error(w, "invalid_client", http.StatusUnauthorized)
		return
	}
	entry, ok := s.authStore.ConsumeRefreshToken(token)
	if !ok || entry.ClientID != clientID {
		http.Error(w, "invalid_grant", http.StatusBadRequest)
		return
	}
	resp, err := s.issueTokens(clientID, entry.Scope, entry.Patient, entry.Subject)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		Scope                   string   `json:"scope"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		ClientName              string   `json:"client_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	grantTypes := defaultIfEmpty(req.GrantTypes, []string{"authorization_code", "refresh_token", "client_credentials"})
	if requiresRedirectURI(grantTypes) && len(req.RedirectURIs) == 0 {
		http.Error(w, "redirect_uris required for authorization_code grant", http.StatusBadRequest)
		return
	}
	clientID := randomToken()
	secret := generateClientSecret()
	client := Client{
		ClientID:                clientID,
		ClientSecret:            secret,
		RedirectURIs:            req.RedirectURIs,
		GrantTypes:              grantTypes,
		ResponseTypes:           defaultIfEmpty(req.ResponseTypes, []string{"code"}),
		Scopes:                  strings.Fields(req.Scope),
		TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
	}
	s.cfg.Clients.Register(client)
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  clientID,
		"client_secret":              secret,
		"redirect_uris":              client.RedirectURIs,
		"grant_types":                client.GrantTypes,
		"response_types":             client.ResponseTypes,
		"token_endpoint_auth_method": client.TokenEndpointAuthMethod,
		"scope":                      req.Scope,
	})
}

func (s *Server) issueTokens(clientID, scope, patient, subject string) (map[string]any, error) {
	now := s.cfg.Now()
	exp := now.Add(s.cfg.AccessTokenTTL)
	accessToken, err := buildJWT(map[string]string{
		"alg": s.cfg.SigningKey.Algorithm,
		"kid": s.cfg.SigningKey.KeyID,
		"typ": "JWT",
	}, map[string]any{
		"iss":     s.cfg.Issuer,
		"sub":     subject,
		"aud":     s.cfg.FHIRAudience,
		"iat":     now.Unix(),
		"exp":     exp.Unix(),
		"scope":   scope,
		"patient": patient,
		"jti":     randomToken(),
	}, s.cfg.SigningKey.PrivateKey)
	if err != nil {
		return nil, err
	}
	refresh := randomToken()
	_ = s.authStore.SaveRefreshToken(refresh, RefreshTokenEntry{
		ClientID:  clientID,
		Scope:     scope,
		Patient:   patient,
		Subject:   subject,
		ExpiresAt: now.Add(s.cfg.RefreshTokenTTL),
	})
	return map[string]any{
		"access_token":  accessToken,
		"token_type":    "Bearer",
		"expires_in":    int(s.cfg.AccessTokenTTL.Seconds()),
		"scope":         scope,
		"refresh_token": refresh,
		"patient":       patient,
	}, nil
}

func (s *Server) openIDConfiguration() map[string]any {
	return map[string]any{
		"issuer":                                s.cfg.Issuer,
		"jwks_uri":                              s.cfg.Issuer + "/oauth/jwks",
		"authorization_endpoint":                s.cfg.Issuer + "/oauth/authorize",
		"token_endpoint":                        s.cfg.Issuer + "/oauth/token",
		"registration_endpoint":                 s.cfg.Issuer + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "client_credentials", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post", "private_key_jwt"},
		"scopes_supported":                      smart.DefaultConfiguration(s.cfg.Issuer).ScopesSupported,
	}
}

func redirectAllowed(allowed []string, redirectURI string) bool {
	if len(allowed) == 0 || strings.TrimSpace(redirectURI) == "" {
		return false
	}
	for _, uri := range allowed {
		if uri == redirectURI {
			return true
		}
	}
	return false
}

func requiresRedirectURI(grantTypes []string) bool {
	for _, gt := range grantTypes {
		if gt == "authorization_code" {
			return true
		}
	}
	return false
}

func validateRequestedScopes(client Client, scope string) error {
	if len(client.Scopes) == 0 || strings.TrimSpace(scope) == "" {
		return nil
	}
	allowed, err := smart.ParseScopes(strings.Join(client.Scopes, " "))
	if err != nil {
		return err
	}
	granted, err := smart.ParseScopes(scope)
	if err != nil {
		return err
	}
	if !granted.SubsetOf(allowed) {
		return fmt.Errorf("requested scope not allowed for client")
	}
	return nil
}

func pkceValid(challenge, method, verifier string) bool {
	if verifier == "" {
		return false
	}
	if method == "" || method == "S256" {
		h := sha256.Sum256([]byte(verifier))
		return base64URLEncode(h[:]) == challenge
	}
	return false
}

func defaultIfEmpty(values, fallback []string) []string {
	if len(values) == 0 {
		return fallback
	}
	return values
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

