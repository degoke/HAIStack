package oauth

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

// IntrospectionResponse is the RFC 7662 token introspection payload.
type IntrospectionResponse struct {
	Active    bool   `json:"active"`
	Scope     string `json:"scope,omitempty"`
	ClientID  string `json:"client_id,omitempty"`
	Username  string `json:"username,omitempty"`
	TokenType string `json:"token_type,omitempty"`
	Exp       int64  `json:"exp,omitempty"`
	Iat       int64  `json:"iat,omitempty"`
	Sub       string `json:"sub,omitempty"`
	Aud       string `json:"aud,omitempty"`
	Iss       string `json:"iss,omitempty"`
	JTI       string `json:"jti,omitempty"`
	Patient   string `json:"patient,omitempty"`
	FHIRUser  string `json:"fhirUser,omitempty"`
	Tenant    string `json:"tenant,omitempty"`
}

func (s *Server) IntrospectionEndpoint() string {
	return s.issuer + "/oauth/introspect"
}

func (s *Server) handleIntrospect(w http.ResponseWriter, r *http.Request) {
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
	resp := s.IntrospectToken(token, hint)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

// IntrospectToken validates a token and returns RFC 7662 metadata.
func (s *Server) IntrospectToken(token, tokenTypeHint string) IntrospectionResponse {
	if strings.TrimSpace(token) == "" {
		return IntrospectionResponse{Active: false}
	}
	switch tokenTypeHint {
	case "refresh_token":
		if resp, ok := s.introspectRefreshToken(token); ok {
			return resp
		}
	case "access_token":
		if resp, ok := s.introspectAccessToken(token); ok {
			return resp
		}
	}
	if resp, ok := s.introspectAccessToken(token); ok {
		return resp
	}
	if resp, ok := s.introspectRefreshToken(token); ok {
		return resp
	}
	return IntrospectionResponse{Active: false}
}

func (s *Server) introspectAccessToken(token string) (IntrospectionResponse, bool) {
	verifier := s.Verifier()
	opts := smart.TokenValidateOptions{
		ExpectedIssuer:   s.issuer,
		ExpectedAudience: s.fhirBase,
	}
	claims, err := smart.ValidateToken(token, verifier, opts)
	if err != nil {
		return IntrospectionResponse{}, false
	}
	if s.revocation != nil && claims.JWTID != "" {
		revoked, err := s.revocation.IsRevoked(claims.JWTID)
		if err != nil || revoked {
			return IntrospectionResponse{Active: false}, true
		}
	}
	return claimsToIntrospection(claims, "Bearer"), true
}

func (s *Server) introspectRefreshToken(token string) (IntrospectionResponse, bool) {
	if s.refresh == nil {
		return IntrospectionResponse{}, false
	}
	record, err := s.refresh.Lookup(token)
	if err != nil {
		return IntrospectionResponse{}, false
	}
	return IntrospectionResponse{
		Active:    true,
		Scope:     record.Scope,
		ClientID:  record.ClientID,
		Username:  record.Subject,
		TokenType: "refresh_token",
		Exp:       record.ExpiresAt.Unix(),
		Sub:       record.Subject,
		Iss:       s.issuer,
		Patient:   record.Patient,
		FHIRUser:  record.FHIRUser,
		Tenant:    record.TenantHint,
	}, true
}

func claimsToIntrospection(claims smart.TokenClaims, tokenType string) IntrospectionResponse {
	resp := IntrospectionResponse{
		Active:    true,
		Scope:     claims.Scope,
		ClientID:  firstNonEmpty(claims.ClientID, claims.Subject),
		Username:  claims.Subject,
		TokenType: tokenType,
		Sub:       claims.Subject,
		Iss:       claims.Issuer,
		JTI:       claims.JWTID,
		Patient:   claims.Patient,
		FHIRUser:  claims.FHIRUser,
		Tenant:    claims.TenantHint,
	}
	if !claims.ExpiresAt.IsZero() {
		resp.Exp = claims.ExpiresAt.Unix()
	}
	if !claims.IssuedAt.IsZero() {
		resp.Iat = claims.IssuedAt.Unix()
	}
	if len(claims.Audience) > 0 {
		resp.Aud = claims.Audience[0]
	}
	return resp
}
