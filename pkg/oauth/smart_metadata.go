package oauth

import (
	"encoding/json"
	"net/http"
)

type smartConfiguration struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RevocationEndpoint                string   `json:"revocation_endpoint,omitempty"`
	IntrospectionEndpoint             string   `json:"introspection_endpoint,omitempty"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported            []string `json:"response_types_supported,omitempty"`
	GrantTypesSupported               []string `json:"grant_types_supported,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`
	Capabilities                      []string `json:"capabilities,omitempty"`
}

func (s *Server) handleSmartConfiguration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	cfg := s.smartConfiguration()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
}

func (s *Server) smartConfiguration() smartConfiguration {
	return smartConfiguration{
		Issuer:                        s.issuer,
		AuthorizationEndpoint:         s.AuthorizationEndpoint(),
		TokenEndpoint:                 s.TokenEndpoint(),
		RevocationEndpoint:            s.RevocationEndpoint(),
		IntrospectionEndpoint:         s.IntrospectionEndpoint(),
		ScopesSupported:               append([]string(nil), s.scopes...),
		ResponseTypesSupported:        []string{"code"},
		GrantTypesSupported:           s.grantTypesSupported(),
		CodeChallengeMethodsSupported: []string{"S256"},
		Capabilities:                  []string{"launch-standalone", "client-public"},
	}
}

// SMARTConfiguration returns the discovery document for programmatic use.
func (s *Server) SMARTConfiguration() smartConfiguration {
	return s.smartConfiguration()
}
