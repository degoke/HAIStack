package oauth

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Patient      string `json:"patient,omitempty"`
	Encounter    string `json:"encounter,omitempty"`
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed form body")
		return
	}
	grantType := strings.TrimSpace(r.Form.Get("grant_type"))
	switch grantType {
	case "authorization_code":
		s.handleAuthorizationCodeGrant(w, r)
	case "client_credentials":
		s.handleClientCredentialsGrant(w, r)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type not supported")
	}
}

func (s *Server) handleAuthorizationCodeGrant(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.Form.Get("code"))
	clientID := strings.TrimSpace(r.Form.Get("client_id"))
	redirectURI := strings.TrimSpace(r.Form.Get("redirect_uri"))
	codeVerifier := strings.TrimSpace(r.Form.Get("code_verifier"))
	clientSecret := strings.TrimSpace(r.Form.Get("client_secret"))

	if code == "" || clientID == "" || redirectURI == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code, client_id, and redirect_uri are required")
		return
	}

	client, err := s.clients.Lookup(clientID)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client", "unknown client")
		return
	}
	if client.Confidential {
		if clientSecret == "" || clientSecret != client.ClientSecret {
			writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "client authentication failed")
			return
		}
	}

	authCode, err := s.codes.Exchange(code, clientID, redirectURI, codeVerifier)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}

	subject := authCode.User
	if subject == "" {
		subject = clientID
	}
	fhirUser := ""
	if subject != "" && !strings.HasPrefix(subject, "http") {
		fhirUser = subject
	}
	token, exp, err := IssueAccessToken(s.signer, s.issuer, AccessTokenClaims{
		Subject:    subject,
		ClientID:   clientID,
		Scope:      authCode.Scope,
		Audience:   s.fhirBase,
		Patient:    authCode.Patient,
		TenantHint: firstNonEmpty(authCode.TenantHint, client.TenantHint),
		FHIRUser:   fhirUser,
		TTL:        s.tokenTTL,
	})
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue access token")
		return
	}
	writeTokenResponse(w, tokenResponse{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int(time.Until(exp).Seconds()),
		Scope:       authCode.Scope,
		Patient:     authCode.Patient,
	})
}

func (s *Server) handleClientCredentialsGrant(w http.ResponseWriter, r *http.Request) {
	if s.backendAuth == nil {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "client_credentials not configured")
		return
	}
	assertionType := strings.TrimSpace(r.Form.Get("client_assertion_type"))
	assertion := strings.TrimSpace(r.Form.Get("client_assertion"))
	scope := strings.TrimSpace(r.Form.Get("scope"))
	if assertionType != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" || assertion == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_assertion required")
		return
	}
	claims, backendClient, err := s.backendAuth.ValidateBackendAssertion(assertion, smart.TokenValidateOptions{})
	if err != nil {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", err.Error())
		return
	}
	grantedScope := claims.Scope
	if scope != "" {
		requested, err := smart.ParseScopes(scope)
		if err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_scope", err.Error())
			return
		}
		if err := s.backendAuth.AuthorizeBackendScopes(backendClient.ClientID, requested); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_scope", err.Error())
			return
		}
		grantedScope = requested.SpaceSeparated()
	}
	token, exp, err := IssueAccessToken(s.signer, s.issuer, AccessTokenClaims{
		Subject:    backendClient.ClientID,
		ClientID:   backendClient.ClientID,
		Scope:      grantedScope,
		Audience:   s.fhirBase,
		TenantHint: firstNonEmpty(claims.TenantHint, backendClient.TenantHint),
		TTL:        s.tokenTTL,
	})
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue access token")
		return
	}
	writeTokenResponse(w, tokenResponse{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int(time.Until(exp).Seconds()),
		Scope:       grantedScope,
	})
}

func writeTokenResponse(w http.ResponseWriter, resp tokenResponse) {
	if resp.ExpiresIn <= 0 {
		resp.ExpiresIn = int(defaultAccessTokenTTL.Seconds())
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	_ = json.NewEncoder(w).Encode(resp)
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             code,
		"error_description": description,
	})
}

func writeMethodNotAllowed(w http.ResponseWriter, allowed ...string) {
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
