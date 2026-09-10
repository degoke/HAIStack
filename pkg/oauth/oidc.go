package oauth

import (
	"encoding/json"
	"net/http"
)

type openIDConfiguration struct {
	Issuer                           string   `json:"issuer"`
	AuthorizationEndpoint            string   `json:"authorization_endpoint"`
	TokenEndpoint                    string   `json:"token_endpoint"`
	RevocationEndpoint               string   `json:"revocation_endpoint,omitempty"`
	IntrospectionEndpoint            string   `json:"introspection_endpoint,omitempty"`
	JWKSURI                          string   `json:"jwks_uri,omitempty"`
	ResponseTypesSupported           []string `json:"response_types_supported,omitempty"`
	SubjectTypesSupported            []string `json:"subject_types_supported,omitempty"`
	IDTokenSigningAlgValuesSupported []string `json:"id_token_signing_alg_values_supported,omitempty"`
	ScopesSupported                  []string `json:"scopes_supported,omitempty"`
	GrantTypesSupported              []string `json:"grant_types_supported,omitempty"`
	CodeChallengeMethodsSupported    []string `json:"code_challenge_methods_supported,omitempty"`
}

func (s *Server) handleOpenIDConfiguration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	cfg := s.openIDConfiguration()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
}

func (s *Server) openIDConfiguration() openIDConfiguration {
	grants := s.grantTypesSupported()
	cfg := openIDConfiguration{
		Issuer:                           s.issuer,
		AuthorizationEndpoint:            s.AuthorizationEndpoint(),
		TokenEndpoint:                    s.TokenEndpoint(),
		RevocationEndpoint:               s.RevocationEndpoint(),
		IntrospectionEndpoint:            s.IntrospectionEndpoint(),
		ResponseTypesSupported:           []string{"code"},
		SubjectTypesSupported:            []string{"public"},
		IDTokenSigningAlgValuesSupported: []string{s.signer.Algorithm()},
		ScopesSupported:                  append([]string(nil), s.scopes...),
		GrantTypesSupported:              grants,
		CodeChallengeMethodsSupported:    []string{"S256"},
	}
	if _, ok := s.signer.(interface{ PublicJWKS() ([]byte, error) }); ok {
		cfg.JWKSURI = s.issuer + "/.well-known/jwks.json"
	}
	return cfg
}

func (s *Server) grantTypesSupported() []string {
	grants := []string{"authorization_code", "refresh_token"}
	if s.backendAuth != nil {
		grants = append(grants, "client_credentials")
	}
	return grants
}
