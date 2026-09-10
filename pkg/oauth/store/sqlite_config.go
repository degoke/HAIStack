package store

import (
	"database/sql"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

// ApplySQLiteStores wires SQLite-backed OAuth persistence into cfg.
// Apply pkg/sqlite migrations 0012_oauth.sql through 0019_oauth_signing_key.sql before use.
// Call this before oauth.NewServer for production-like hosts; without it, auth codes
// and other OAuth state remain in-memory (see oauth.UsesInMemoryStores).
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
	cfg.ConsentSessionStore = stores.Consent
	cfg.ClientStore = stores.Clients
	if cfg.Clients != nil && cfg.Issuer != "" {
		if err := oauth.SeedClientStore(stores.Clients, cfg.Issuer, cfg.Clients); err != nil {
			return err
		}
	}
	return nil
}
