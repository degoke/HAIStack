package store

import (
	"database/sql"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

// ApplySQLiteStores wires SQLite-backed OAuth persistence into cfg.
// Apply pkg/sqlite migrations 0012_oauth.sql, 0013_oauth_launch.sql,
// 0014_oauth_auth_code_issuer.sql, and 0015_oauth_launch_issuer.sql before use. Call this before oauth.NewServer
// for production-like hosts; without it, auth codes and other OAuth state remain
// in-memory (see oauth.UsesInMemoryStores).
func ApplySQLiteStores(cfg *oauth.Config, db *sql.DB) error {
	if cfg == nil {
		return oauth.ErrInvalidConfig
	}
	stores, err := NewSQLiteStores(db)
	if err != nil {
		return err
	}
	cfg.CodeStore = stores.Codes
	cfg.RefreshStore = stores.Refresh
	cfg.RevocationStore = stores.Revocation
	cfg.LaunchStore = stores.Launch
	return nil
}
