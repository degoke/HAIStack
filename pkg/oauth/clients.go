package oauth

import (
	"fmt"
	"strings"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

// Client describes a registered OAuth client for the built-in authorization server.
type Client struct {
	smart.ClientRegistration

	// Confidential marks clients that require a client secret at the token endpoint.
	Confidential bool `json:"confidential,omitempty"`
	// ClientSecret is required for confidential clients when exchanging codes.
	ClientSecret string `json:"clientSecret,omitempty"`
	// AllowedScopes restricts requested scopes to this allow-list. When empty,
	// Client.Scopes from ClientRegistration is used.
	AllowedScopes []string `json:"allowedScopes,omitempty"`
	// TenantHint seeds tenant context on issued tokens.
	TenantHint string `json:"tenantHint,omitempty"`
	// DefaultPatient is placed in the patient launch claim for standalone demos.
	DefaultPatient string `json:"defaultPatient,omitempty"`
	// DefaultUser is placed in sub/fhirUser for user-context tokens.
	DefaultUser string `json:"defaultUser,omitempty"`
}

// ClientRegistry stores registered OAuth clients.
type ClientRegistry struct {
	mu      sync.RWMutex
	clients map[string]Client
}

// NewClientRegistry returns an empty client registry.
func NewClientRegistry() *ClientRegistry {
	return &ClientRegistry{clients: make(map[string]Client)}
}

// Register upserts a client. Redirect URIs and scopes are validated.
func (r *ClientRegistry) Register(c Client) error {
	if r == nil {
		return fmt.Errorf("%w: registry is nil", ErrInvalidConfig)
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return fmt.Errorf("%w: client id required", ErrInvalidConfig)
	}
	allowed := c.AllowedScopes
	if len(allowed) == 0 {
		allowed = c.Scopes
	}
	if len(allowed) > 0 {
		if _, err := smart.ParseScopes(strings.Join(allowed, " ")); err != nil {
			return fmt.Errorf("%w: client %q scopes: %v", ErrInvalidConfig, c.ClientID, err)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.clients == nil {
		r.clients = make(map[string]Client)
	}
	r.clients[c.ClientID] = c
	return nil
}

// Lookup returns a registered client.
func (r *ClientRegistry) Lookup(clientID string) (Client, error) {
	if r == nil {
		return Client{}, fmt.Errorf("%w: %s", ErrInvalidClient, clientID)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[clientID]
	if !ok {
		return Client{}, fmt.Errorf("%w: %s", ErrInvalidClient, clientID)
	}
	return c, nil
}

// allowedScopeSet returns the parsed allow-list for a client.
func (c Client) allowedScopeSet() (smart.ScopeSet, error) {
	allowed := c.AllowedScopes
	if len(allowed) == 0 {
		allowed = c.Scopes
	}
	if len(allowed) == 0 {
		return smart.ScopeSet{}, nil
	}
	return smart.ParseScopes(strings.Join(allowed, " "))
}

// authorizeScopes checks that requested scopes are a subset of the client allow-list.
func (c Client) authorizeScopes(requested string) (string, error) {
	req := strings.TrimSpace(requested)
	if req == "" {
		allowed, err := c.allowedScopeSet()
		if err != nil {
			return "", err
		}
		return allowed.SpaceSeparated(), nil
	}
	granted, err := smart.ParseScopes(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidScope, err)
	}
	allowed, err := c.allowedScopeSet()
	if err != nil {
		return "", err
	}
	if allowed.Empty() {
		return granted.SpaceSeparated(), nil
	}
	if !granted.SubsetOf(allowed) {
		return "", fmt.Errorf("%w: requested %v not in allow-list %v", ErrInvalidScope, granted.Strings(), allowed.Strings())
	}
	return granted.SpaceSeparated(), nil
}

// redirectAllowed reports whether redirectURI exactly matches a registered URI.
func (c Client) redirectAllowed(redirectURI string) bool {
	for _, u := range c.RedirectURIs {
		if u == redirectURI {
			return true
		}
	}
	return false
}

// requiresPKCE reports whether PKCE is mandatory for this client.
func (c Client) requiresPKCE() bool {
	// Public clients always require PKCE; confidential clients may omit it.
	return !c.Confidential
}
