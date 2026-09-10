package oauth

import (
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

type clientStoreBridge struct {
	store ClientRegistry
}

func (b clientStoreBridge) LookupBackendClient(clientID string) (smart.BackendClient, error) {
	if b.store == nil {
		return smart.BackendClient{}, fmt.Errorf("%w: client %q", smart.ErrClientNotAllowed, clientID)
	}
	client, ok := b.store.Get(clientID)
	if !ok || client.PublicKeyPEM == "" {
		return smart.BackendClient{}, fmt.Errorf("%w: client %q", smart.ErrClientNotAllowed, clientID)
	}
	return client.ToBackendClient(), nil
}

func (b clientStoreBridge) RegisterBackendClient(client smart.BackendClient) error {
	if b.store == nil {
		return fmt.Errorf("oauth: client store is nil")
	}
	return b.store.Register(Client{
		ClientID:     client.ClientID,
		Scopes:       client.AllowedScopes,
		PublicKeyPEM: client.Key.PublicKeyPEM,
		KeyID:        client.Key.KeyID,
		Algorithm:    client.Key.Algorithm,
	})
}
