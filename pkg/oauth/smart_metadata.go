package oauth

import (
	"encoding/json"
	"net/http"
)

type smartConfiguration struct {
	Issuer                 string   `json:"issuer"`
	AuthorizationEndpoint  string   `json:"authorization_endpoint"`
	TokenEndpoint          string   `json:"token_endpoint"`
	RevocationEndpoint     string   `json:"revocation_endpoint,omitempty"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported []string `json:"response_types_supported,omitempty"`
	GrantTypesSupported    []string `json:"grant_types_supported,omitempty"`
	CodeChallengeMethods   []string `json:"code_challenge_methods_supported,omitempty"`
	Capabilities           []string `json:"capabilities,omitempty"`
}

func (s *Server) handleSmartConfiguration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	grants := []string{"authorization_code"}
	if s.backendAuth != nil {
		grants = append(grants, "client_credentials")
	}
	cfg := smartConfiguration{
		Issuer:                 s.issuer,
		AuthorizationEndpoint:  s.AuthorizationEndpoint(),
		TokenEndpoint:          s.TokenEndpoint(),
		ScopesSupported:        append([]string(nil), s.scopes...),
		ResponseTypesSupported: []string{"code"},
		GrantTypesSupported:    grants,
		CodeChallengeMethods:   []string{"S256"},
		Capabilities:           []string{"launch-standalone", "client-public"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
}

// SMARTConfiguration returns the discovery document for programmatic use.
func (s *Server) SMARTConfiguration() smartConfiguration {
	grants := []string{"authorization_code"}
	if s.backendAuth != nil {
		grants = append(grants, "client_credentials")
	}
	return smartConfiguration{
		Issuer:                 s.issuer,
		AuthorizationEndpoint:  s.AuthorizationEndpoint(),
		TokenEndpoint:          s.TokenEndpoint(),
		ScopesSupported:        append([]string(nil), s.scopes...),
		ResponseTypesSupported: []string{"code"},
		GrantTypesSupported:    grants,
		CodeChallengeMethods:   []string{"S256"},
		Capabilities:           []string{"launch-standalone", "client-public"},
	}
}
