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
	if err := bindUnscopedPostgresClients(pool, cfg.Issuer); err != nil {
		return err
	}
	cfg.AuthorizationStore = authStore
	cfg.Clients = clientRegistryForIssuer(clientStore, cfg.Issuer)
	cfg.ReplayStore = replayStore
	cfg.RevocationStore = revocationStore
	cfg.TokenRateLimiter = &PostgresRateLimitStore{Pool: pool}
	cfg.RegisterRateLimiter = &PostgresRateLimitStore{Pool: pool}
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
	if err := bindUnscopedSQLiteClients(db, cfg.Issuer); err != nil {
		return err
	}
	cfg.AuthorizationStore = authStore
	cfg.Clients = clientRegistryForIssuer(clientStore, cfg.Issuer)
	cfg.ReplayStore = replayStore
	cfg.RevocationStore = revocationStore
	cfg.TokenRateLimiter = &SQLiteRateLimitStore{DB: db}
	cfg.RegisterRateLimiter = &SQLiteRateLimitStore{DB: db}
	return nil
}

// ApplyPostgresSigningKey loads or creates DB-backed signing keys for issuer.
func ApplyPostgresSigningKey(cfg *oauth.Config, pool *pgxpool.Pool, issuer string, opts SigningKeyOptions) error {
	if cfg == nil {
		return fmt.Errorf("oauth/store: config is required")
	}
	if opts.VerificationTTL <= 0 && cfg.AccessTokenTTL > 0 {
		opts.VerificationTTL = cfg.AccessTokenTTL
	}
	set, err := LoadOrCreatePostgresSigningKeySet(pool, issuer, opts)
	if err != nil {
		return err
	}
	cfg.SigningKey = set.Active
	cfg.VerificationKeys = verificationKeysWithoutActive(set)
	return nil
}

// ApplySQLiteSigningKey loads or creates DB-backed signing keys for issuer.
func ApplySQLiteSigningKey(cfg *oauth.Config, db *sql.DB, issuer string, opts SigningKeyOptions) error {
	if cfg == nil {
		return fmt.Errorf("oauth/store: config is required")
	}
	if opts.VerificationTTL <= 0 && cfg.AccessTokenTTL > 0 {
		opts.VerificationTTL = cfg.AccessTokenTTL
	}
	set, err := LoadOrCreateSQLiteSigningKeySet(db, issuer, opts)
	if err != nil {
		return err
	}
	cfg.SigningKey = set.Active
	cfg.VerificationKeys = verificationKeysWithoutActive(set)
	return nil
}

func verificationKeysWithoutActive(set SigningKeySet) []*oauth.KeySet {
	if set.Active == nil {
		return set.Verification
	}
	out := make([]*oauth.KeySet, 0, len(set.Verification))
	for _, key := range set.Verification {
		if key == nil || key.KeyID == set.Active.KeyID {
			continue
		}
		out = append(out, key)
	}
	return out
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

// NewSQLiteServer applies SQLite stores and constructs an authorization server.
func NewSQLiteServer(cfg oauth.Config, db *sql.DB) (*oauth.Server, error) {
	if err := ApplySQLiteStores(&cfg, db); err != nil {
		return nil, err
	}
	return NewServer(cfg)
}

func clientRegistryForIssuer(reg oauth.ClientRegistry, issuer string) oauth.ClientRegistry {
	if scoped, ok := reg.(oauth.IssuerScopedClientRegistry); ok {
		return scoped.ForIssuer(issuer)
	}
	return reg
}
