package redis

import (
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	goredis "github.com/redis/go-redis/v9"
)

// NewServer constructs an authorization server with Redis-backed stores.
func NewServer(cfg oauth.Config, client goredis.Cmdable, keyPrefix string) (*oauth.Server, error) {
	if client == nil {
		return nil, fmt.Errorf("oauth/redis: client is required")
	}
	authStore, clientStore, replayStore, revocationStore := Stores(client, keyPrefix)
	cfg.AuthorizationStore = authStore
	cfg.Clients = clientStore
	cfg.ReplayStore = replayStore
	cfg.RevocationStore = revocationStore
	return oauth.NewServer(cfg)
}
