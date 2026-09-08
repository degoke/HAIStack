package smart

// Configuration is the SMART App Launch metadata shape hosts may serve at
// {fhir-base}/.well-known/smart-configuration.
type Configuration struct {
	Issuer                        string   `json:"issuer,omitempty"`
	JWKSURI                       string   `json:"jwks_uri,omitempty"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint,omitempty"`
	TokenEndpoint                 string   `json:"token_endpoint,omitempty"`
	RegistrationEndpoint          string   `json:"registration_endpoint,omitempty"`
	RevocationEndpoint            string   `json:"revocation_endpoint,omitempty"`
	IntrospectionEndpoint         string   `json:"introspection_endpoint,omitempty"`
	ScopesSupported               []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported        []string `json:"response_types_supported,omitempty"`
	GrantTypesSupported           []string `json:"grant_types_supported,omitempty"`
	Capabilities                  []string `json:"capabilities,omitempty"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported,omitempty"`
}

// DefaultConfiguration returns a baseline SMART 1.x/2.x compatible configuration
// document. Hosts should override endpoints and supported scopes for their deployment.
func DefaultConfiguration(issuer string) Configuration {
	return Configuration{
		Issuer: issuer,
		ScopesSupported: []string{
			"openid", "fhirUser", "launch", "launch/patient",
			"patient/*.read", "patient/*.write",
			"user/*.read", "user/*.write",
			"system/*.read", "system/*.write",
		},
		ResponseTypesSupported:        []string{"code"},
		GrantTypesSupported:           []string{"authorization_code", "client_credentials"},
		Capabilities:                  defaultCapabilities(),
		CodeChallengeMethodsSupported: []string{"S256"},
	}
}

func defaultCapabilities() []string {
	return []string{
		"launch-ehr", "launch-standalone", "client-public",
		"client-confidential-symmetric", "client-confidential-asymmetric",
		"context-ehr-patient", "context-standalone-patient",
		"permission-patient", "permission-user", "permission-v1", "permission-v2",
	}
}

// SMARTVersion documents the SMART scope patterns implemented by pkg/smart v1.
const SMARTVersion = "1.0 patterns (SMART App Launch 2.2 granular scopes deferred)"
