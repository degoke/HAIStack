package oauth

import (
	"sync"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

// Client describes a registered OAuth client.
type Client struct {
	ClientID                string
	ClientSecret            string
	ClientSecretHash        string
	ClientName              string
	RedirectURIs            []string
	GrantTypes              []string
	ResponseTypes           []string
	Scopes                  []string
	TokenEndpointAuthMethod string
	PublicKeyPEM            string
	KeyID                   string
	Algorithm               string
}

// ClientStore stores registered OAuth clients in memory.
type ClientStore struct {
	mu      sync.RWMutex
	clients map[string]Client
}

// NewClientStore constructs an empty client store.
func NewClientStore() *ClientStore {
	return &ClientStore{clients: make(map[string]Client)}
}

// Register adds or replaces a client.
func (s *ClientStore) Register(client Client) error {
	if s == nil {
		return nil
	}
	if err := prepareClientSecret(&client); err != nil {
		return err
	}
	s.mu.Lock()
	s.clients[client.ClientID] = client
	s.mu.Unlock()
	return nil
}

// Get returns a registered client.
func (s *ClientStore) Get(clientID string) (Client, bool) {
	if s == nil {
		return Client{}, false
	}
	s.mu.RLock()
	client, ok := s.clients[clientID]
	s.mu.RUnlock()
	return client, ok
}

// ToBackendClient converts client key metadata for backend assertion validation.
func (c Client) ToBackendClient() smart.BackendClient {
	return smart.BackendClient{
		ClientID: c.ClientID,
		AllowedScopes: c.Scopes,
		Key: smart.ClientKeyMetadata{
			Algorithm:    c.Algorithm,
			PublicKeyPEM: c.PublicKeyPEM,
			KeyID:        c.KeyID,
		},
	}
}
