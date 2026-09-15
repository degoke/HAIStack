package store

import (
	"database/sql"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ApplyPostgresStores wires Postgres-backed OAuth stores into cfg.
func ApplyPostgresStores(cfg *oauth.Config, pool *pgxpool.Pool) error {
	if cfg == nil {
		return fmt.Errorf("oauth/store: config is required")
	}
	if pool == nil {
		return fmt.Errorf("oauth/store: postgres pool is required")
	}
	authStore, clientStore, replayStore, revocationStore := PostgresStores(pool)
	cfg.AuthorizationStore = authStore
	cfg.Clients = clientStore
	cfg.ReplayStore = replayStore
	cfg.RevocationStore = revocationStore
	return nil
}

// ApplySQLiteStores wires SQLite-backed OAuth stores into cfg.
func ApplySQLiteStores(cfg *oauth.Config, db *sql.DB) error {
	if cfg == nil {
		return fmt.Errorf("oauth/store: config is required")
	}
	if db == nil {
		return fmt.Errorf("oauth/store: sqlite db is required")
	}
	authStore, clientStore, replayStore, revocationStore := SQLiteStores(db)
	cfg.AuthorizationStore = authStore
	cfg.Clients = clientStore
	cfg.ReplayStore = replayStore
	cfg.RevocationStore = revocationStore
	return nil
}

// NewServer constructs an authorization server after stores are applied to cfg.
func NewServer(cfg oauth.Config) (*oauth.Server, error) {
	return oauth.NewServer(cfg)
}

// NewPostgresServer applies Postgres stores and constructs an authorization server.
func NewPostgresServer(cfg oauth.Config, pool *pgxpool.Pool) (*oauth.Server, error) {
	if err := ApplyPostgresStores(&cfg, pool); err != nil {
		return nil, err
	}
	return NewServer(cfg)
}
