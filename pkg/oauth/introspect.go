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
	if _, ok := s.requireConfidentialClient(w, r); !ok {
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
	pem, err := s.cfg.SigningKey.PublicKeyPEM()
	if err != nil {
		return IntrospectionResponse{}, false
	}
	verifier := smart.PEMVerifier{
		PublicKeyPEM: pem,
		Algorithm:    s.cfg.SigningKey.Algorithm,
	}
	opts := smart.TokenValidateOptions{
		ExpectedIssuer:   s.cfg.Issuer,
		ExpectedAudience: s.cfg.FHIRAudience,
		RequireIssuer:    true,
		RequireAudience:  true,
		RequireExpiry:    true,
		RequireSubject:   true,
		Now:              s.cfg.Now,
	}
	if s.revocationStore != nil {
		opts.IsJWTRevoked = s.revocationStore.IsRevoked
	}
	claims, err := smart.ValidateToken(token, verifier, opts)
	if err != nil {
		return IntrospectionResponse{}, false
	}
	return claimsToIntrospection(claims, "Bearer"), true
}

func (s *Server) introspectRefreshToken(token string) (IntrospectionResponse, bool) {
	if s.authStore == nil {
		return IntrospectionResponse{}, false
	}
	record, ok := s.authStore.LookupRefreshToken(s.cfg.Issuer, token)
	if !ok {
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
		Iss:       s.cfg.Issuer,
		Patient:   record.Patient,
		FHIRUser:  record.FHIRUser,
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
