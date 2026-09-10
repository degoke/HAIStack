package smart

// Configuration is the SMART App Launch metadata shape served at
// /.well-known/smart-configuration by pkg/oauth or host integrations.
type Configuration struct {
	Issuer                        string   `json:"issuer"`
	JWKSURI                       string   `json:"jwks_uri,omitempty"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint,omitempty"`
	TokenEndpoint                 string   `json:"token_endpoint,omitempty"`
	RegistrationEndpoint          string   `json:"registration_endpoint,omitempty"`
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
			"patient/*.rs", "patient/Observation.rs?category=laboratory",
			"user/*.rs", "user/*.cud", "user/*.cruds",
			"system/*.rs", "system/*.cud", "system/*.cruds",
			// v1 compatibility patterns
			"patient/*.read", "user/*.read", "user/*.write", "system/*.read", "system/*.write",
		},
		ResponseTypesSupported: []string{"code"},
		GrantTypesSupported:    []string{"authorization_code", "client_credentials"},
		Capabilities: []string{
			"launch-ehr", "launch-standalone", "client-public",
			"client-confidential-symmetric", "client-confidential-asymmetric",
			"context-ehr-patient", "context-standalone-patient",
			"sso-openid-connect", "permission-patient", "permission-user", "permission-offline",
			"permission-v2", "permission-v2.2",
		},
		CodeChallengeMethodsSupported: []string{"S256"},
	}
}

// SMARTVersion documents the SMART scope patterns implemented by pkg/smart.
const SMARTVersion = "2.2 granular scopes (CRUDS + search-parameter filters); v1 read/write patterns supported"
