package redis

import (
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	goredis "github.com/redis/go-redis/v9"
)

// NewServer constructs an authorization server with Redis-backed ephemeral stores.
// cfg.Clients must be set to a durable registry (Postgres or file); Redis holds
// auth codes, refresh tokens, replay JTIs, and revocation denylist only.
func NewServer(cfg oauth.Config, client goredis.Cmdable, keyPrefix string) (*oauth.Server, error) {
	if client == nil {
		return nil, fmt.Errorf("oauth/redis: client is required")
	}
	if cfg.Clients == nil {
		return nil, fmt.Errorf("oauth/redis: cfg.Clients is required (use postgres or file client registry)")
	}
	authStore, replayStore, revocationStore := EphemeralStores(client, keyPrefix)
	cfg.AuthorizationStore = authStore
	cfg.ReplayStore = replayStore
	cfg.RevocationStore = revocationStore
	return oauth.NewServer(cfg)
}
