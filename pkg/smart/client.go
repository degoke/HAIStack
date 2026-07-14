package smart

// ClientRegistration is a minimal static description of a SMART client for
// future expansion. v1 does not implement dynamic client registration or
// launch/session runtime; hosts may store these fields alongside BackendClient.
type ClientRegistration struct {
	ClientID                string   `json:"clientId"`
	ClientName              string   `json:"clientName,omitempty"`
	RedirectURIs            []string `json:"redirectUris,omitempty"`
	GrantTypes              []string `json:"grantTypes,omitempty"`
	ResponseTypes           []string `json:"responseTypes,omitempty"`
	Scopes                  []string `json:"scopes,omitempty"`
	TokenEndpointAuthMethod string   `json:"tokenEndpointAuthMethod,omitempty"`
	JWKSURI                 string   `json:"jwksUri,omitempty"`
	Contacts                []string `json:"contacts,omitempty"`
}
