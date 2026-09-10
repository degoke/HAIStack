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
	// ClientSecretHash holds a bcrypt hash loaded from persistent stores.
	ClientSecretHash string `json:"-"`
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

// ClientStore persists registered OAuth clients (static bootstrap or dynamic registration).
type ClientStore interface {
	Register(issuer string, client Client) error
	Upsert(issuer string, client Client) error
	Lookup(issuer, clientID string) (Client, error)
}

// ClientRegistry stores registered OAuth clients in memory.
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
	if err := validateClientRegistration(c); err != nil {
		return err
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

type registryClientStore struct {
	reg *ClientRegistry
}

func (s registryClientStore) Register(_ string, client Client) error {
	if _, err := s.reg.Lookup(client.ClientID); err == nil {
		return fmt.Errorf("%w: %s", ErrClientExists, client.ClientID)
	}
	return s.reg.Register(client)
}

func (s registryClientStore) Upsert(_ string, client Client) error {
	return s.reg.Register(client)
}

func (s registryClientStore) Lookup(_, clientID string) (Client, error) {
	return s.reg.Lookup(clientID)
}

type overlayClientStore struct {
	overlay *ClientRegistry
	base    ClientStore
}

func (s overlayClientStore) Register(issuer string, client Client) error {
	return s.base.Register(issuer, client)
}

func (s overlayClientStore) Upsert(issuer string, client Client) error {
	return s.base.Upsert(issuer, client)
}

func (s overlayClientStore) Lookup(issuer, clientID string) (Client, error) {
	if s.overlay != nil {
		if client, err := s.overlay.Lookup(clientID); err == nil {
			return client, nil
		}
	}
	return s.base.Lookup(issuer, clientID)
}

// Seed copies all clients from a static registry into a persistent store.
func SeedClientStore(store ClientStore, issuer string, reg *ClientRegistry) error {
	if store == nil || reg == nil {
		return nil
	}
	for _, client := range reg.List() {
		if err := store.Upsert(issuer, client); err != nil {
			return err
		}
	}
	return nil
}

// List returns all registered clients (for seeding persistent stores).
func (r *ClientRegistry) List() []Client {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Client, 0, len(r.clients))
	for _, c := range r.clients {
		out = append(out, c)
	}
	return out
}

// ValidateClientRegistration checks static or dynamic client metadata.
func ValidateClientRegistration(c Client) error {
	return validateClientRegistration(c)
}

func validateClientRegistration(c Client) error {
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
	return nil
}

func verifyClientSecret(client Client, secret string) bool {
	if !client.Confidential {
		return secret == ""
	}
	if client.ClientSecretHash != "" {
		return verifyStoredClientSecret(client.ClientSecretHash, secret)
	}
	return secretEqual(secret, client.ClientSecret)
}

// VerifyClientSecret reports whether secret matches a registered client.
func VerifyClientSecret(client Client, secret string) bool {
	return verifyClientSecret(client, secret)
}

// VerifyStoredClientSecret compares a provided secret against a stored hash or legacy plaintext value.
func VerifyStoredClientSecret(stored, provided string) bool {
	return verifyStoredClientSecret(stored, provided)
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
