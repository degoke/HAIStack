package store

import (
	"database/sql"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

// ApplySQLiteStores wires SQLite-backed OAuth persistence into cfg.
// Apply pkg/sqlite migrations 0012_oauth.sql and 0013_oauth_launch.sql before use.
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
