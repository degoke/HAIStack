package store

import (
	"database/sql"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

// ApplySQLiteStores wires SQLite-backed OAuth persistence into cfg.
// Apply pkg/sqlite migrations 0012_oauth.sql through 0021_oauth_rate_limit.sql before use.
func ApplySQLiteStores(cfg *oauth.Config, db *sql.DB) error {
	if cfg == nil {
		return oauth.ErrInvalidConfig
	}
	return applySQLStores(cfg, wrapSQLDB(db, DialectSQLite), DialectSQLite)
}
