package store

import (
	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// ApplyPostgresStores wires Postgres-backed OAuth persistence into cfg.
// Apply pkg/postgres migrations through 0015_oauth.sql before use.
func ApplyPostgresStores(cfg *oauth.Config, pool *pgxpool.Pool) error {
	if pool == nil {
		return oauth.ErrInvalidConfig
	}
	return ApplyPostgresStoresDB(cfg, wrapSQLDB(stdlib.OpenDBFromPool(pool), DialectPostgres))
}

// ApplyPostgresStoresDB wires Postgres-backed OAuth persistence using an existing SQLDB handle.
func ApplyPostgresStoresDB(cfg *oauth.Config, db SQLDB) error {
	if db == nil {
		return oauth.ErrInvalidConfig
	}
	return applySQLStores(cfg, db, DialectPostgres)
}

func applySQLStores(cfg *oauth.Config, db SQLDB, dialect Dialect) error {
	if cfg == nil {
		return oauth.ErrInvalidConfig
	}
	stores, err := NewSQLStores(db, dialect)
	if err != nil {
		return err
	}
	cfg.CodeStore = stores.Codes
	cfg.RefreshStore = stores.Refresh
	cfg.RevocationStore = stores.Revocation
	cfg.LaunchStore = stores.Launch
	cfg.ConsentSessionStore = stores.Consent
	cfg.ClientStore = stores.Clients
	cfg.RateLimitStore = stores.RateLimit
	if cfg.Clients != nil && cfg.Issuer != "" {
		if err := oauth.SeedClientStore(stores.Clients, cfg.Issuer, cfg.Clients); err != nil {
			return err
		}
	}
	return nil
}
