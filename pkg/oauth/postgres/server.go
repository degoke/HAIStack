package postgres

import (
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewServer constructs an authorization server with Postgres-backed stores.
// This is the recommended production deployment for multi-instance clusters.
func NewServer(cfg oauth.Config, pool *pgxpool.Pool) (*oauth.Server, error) {
	if pool == nil {
		return nil, fmt.Errorf("oauth/postgres: pool is required")
	}
	authStore, clientStore, replayStore, revocationStore := Stores(pool)
	cfg.AuthorizationStore = authStore
	cfg.Clients = clientStore
	cfg.ReplayStore = replayStore
	cfg.RevocationStore = revocationStore
	return oauth.NewServer(cfg)
}
