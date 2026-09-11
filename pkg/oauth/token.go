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
	IDToken      string `json:"id_token,omitempty"`
	Patient      string `json:"patient,omitempty"`
	Encounter    string `json:"encounter,omitempty"`
}

type sessionGrant struct {
	ClientID   string
	Scope      string
	Subject    string
	Patient    string
	Encounter  string
	FHIRUser   string
	TenantHint string
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
	case "refresh_token":
		s.handleRefreshTokenGrant(w, r)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type not supported")
	}
}

func (s *Server) handleAuthorizationCodeGrant(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.Form.Get("code"))
	redirectURI := strings.TrimSpace(r.Form.Get("redirect_uri"))
	codeVerifier := strings.TrimSpace(r.Form.Get("code_verifier"))
	clientID, clientSecret, err := clientCredentialsFromRequest(r)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed form body")
		return
	}

	if code == "" || clientID == "" || redirectURI == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code, client_id, and redirect_uri are required")
		return
	}

	client, err := s.clients.Lookup(s.issuer, clientID)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client", "unknown client")
		return
	}
	if client.Confidential {
		if !verifyClientSecret(client, clientSecret) {
			writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "client authentication failed")
			return
		}
	}

	authCode, err := s.codes.Exchange(code, clientID, redirectURI, codeVerifier)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}
	if authCode.Issuer != s.issuer {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code issuer mismatch")
		return
	}

	subject := authCode.User
	if subject == "" {
		subject = clientID
	}
	fhirUser := authCode.User
	if fhirUser != "" && !strings.HasPrefix(fhirUser, "http") && !strings.Contains(fhirUser, "/") {
		fhirUser = "Practitioner/" + fhirUser
	}
	s.writeSessionTokens(w, sessionGrant{
		ClientID:   clientID,
		Scope:      authCode.Scope,
		Subject:    subject,
		Patient:    authCode.Patient,
		Encounter:  authCode.Encounter,
		FHIRUser:   fhirUser,
		TenantHint: firstNonEmpty(authCode.TenantHint, client.TenantHint),
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
	s.writeSessionTokens(w, sessionGrant{
		ClientID:   backendClient.ClientID,
		Scope:      grantedScope,
		Subject:    backendClient.ClientID,
		TenantHint: firstNonEmpty(claims.TenantHint, backendClient.TenantHint),
	})
}

func (s *Server) handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request) {
	if s.refresh == nil {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "refresh_token not configured")
		return
	}
	refreshToken := strings.TrimSpace(r.Form.Get("refresh_token"))
	clientID, clientSecret, err := clientCredentialsFromRequest(r)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed form body")
		return
	}
	if refreshToken == "" || clientID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "refresh_token and client_id are required")
		return
	}
	client, err := s.clients.Lookup(s.issuer, clientID)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client", "unknown client")
		return
	}
	if client.Confidential {
		if !verifyClientSecret(client, clientSecret) {
			writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "client authentication failed")
			return
		}
	}
	var record *RefreshRecord
	var newRefresh string
	if s.rotateRefresh {
		record, newRefresh, err = s.refresh.Rotate(refreshToken, clientID)
	} else {
		record, err = s.refresh.Lookup(refreshToken)
		newRefresh = refreshToken
	}
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}
	if record.ClientID != clientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "client mismatch")
		return
	}
	if record.Issuer != s.issuer {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token issuer mismatch")
		return
	}
	resp, err := s.buildSessionResponse(*record, newRefresh)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue access token")
		return
	}
	writeTokenResponse(w, resp)
}

func (s *Server) writeSessionTokens(w http.ResponseWriter, grant sessionGrant) {
	resp, err := s.buildSessionResponse(RefreshRecord{
		ClientID:   grant.ClientID,
		Scope:      grant.Scope,
		Subject:    grant.Subject,
		Patient:    grant.Patient,
		Encounter:  grant.Encounter,
		FHIRUser:   grant.FHIRUser,
		TenantHint: grant.TenantHint,
		Issuer:     s.issuer,
	}, "")
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue access token")
		return
	}
	writeTokenResponse(w, resp)
}

func (s *Server) buildSessionResponse(record RefreshRecord, existingRefresh string) (tokenResponse, error) {
	access, exp, err := IssueAccessToken(s.signer, s.issuer, AccessTokenClaims{
		Subject:    record.Subject,
		ClientID:   record.ClientID,
		Scope:      record.Scope,
		Audience:   s.fhirBase,
		Patient:    record.Patient,
		Encounter:  record.Encounter,
		FHIRUser:   record.FHIRUser,
		TenantHint: record.TenantHint,
		TTL:        s.tokenTTL,
	})
	if err != nil {
		return tokenResponse{}, err
	}
	resp := tokenResponse{
		AccessToken: access,
		TokenType:   "Bearer",
		ExpiresIn:   int(time.Until(exp).Seconds()),
		Scope:       record.Scope,
		Patient:     record.Patient,
		Encounter:   record.Encounter,
	}
	if scopeAllowsOpenID(record.Scope) {
		idToken, err := IssueIDToken(s.signer, s.issuer, record.ClientID, AccessTokenClaims{
			Subject:    record.Subject,
			Audience:   record.ClientID,
			FHIRUser:   record.FHIRUser,
			Patient:    record.Patient,
			TenantHint: record.TenantHint,
			TTL:        s.tokenTTL,
		})
		if err != nil {
			return tokenResponse{}, err
		}
		resp.IDToken = idToken
	}
	if scopeAllowsOffline(record.Scope) && s.refresh != nil {
		refreshToken := existingRefresh
		if refreshToken == "" {
			issuer := record.Issuer
			if issuer == "" {
				issuer = s.issuer
			}
			refreshToken, err = s.refresh.Issue(RefreshRecord{
				ClientID:   record.ClientID,
				Scope:      record.Scope,
				Subject:    record.Subject,
				Patient:    record.Patient,
				Encounter:  record.Encounter,
				FHIRUser:   record.FHIRUser,
				TenantHint: record.TenantHint,
				Issuer:     issuer,
				ExpiresAt:  s.now().Add(s.refreshTTL),
			})
			if err != nil {
				return tokenResponse{}, err
			}
		}
		resp.RefreshToken = refreshToken
	}
	return resp, nil
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
