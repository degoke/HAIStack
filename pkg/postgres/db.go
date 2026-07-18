package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB owns the shared Postgres connection pool, migration runner, and tenant accessors.
type DB struct {
	pool *pgxpool.Pool
}

// Open opens a Postgres connection pool at dsn with default pool settings.
func Open(ctx context.Context, dsn string, opts ...Option) (*DB, error) {
	cfg := defaultOptions()
	for _, opt := range opts {
		opt(&cfg)
	}

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	poolConfig.MaxConns = cfg.maxConns
	poolConfig.MinConns = cfg.minConns

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &DB{pool: pool}, nil
}

// Migrate runs embedded numbered SQL migrations.
func (db *DB) Migrate(ctx context.Context) error {
	return runMigrations(ctx, db.pool)
}

// Close closes the underlying connection pool.
func (db *DB) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

// Pool returns the underlying pgx pool for advanced use.
func (db *DB) Pool() *pgxpool.Pool {
	return db.pool
}

// Tenant returns a tenant-scoped database accessor.
func (db *DB) Tenant(tenantID string) *TenantDB {
	return &TenantDB{pool: db.pool, tenantID: tenantID}
}

// DefinitionStore returns the global FHIR definition catalog store.
func (db *DB) DefinitionStore() *DefinitionStore {
	return newDefinitionStore(db.pool)
}

// TerminologyStore returns a terminology store scoped to the supplied tenant.
func (db *DB) TerminologyStore(scopeID string) *TerminologyStore {
	return newTerminologyStore(db.pool, scopeID)
}

// EnsureTenant registers a tenant row if it does not already exist.
func (db *DB) EnsureTenant(ctx context.Context, tenantID string) error {
	_, err := db.pool.Exec(ctx, `
		INSERT INTO tenant (id) VALUES ($1)
		ON CONFLICT (id) DO NOTHING`, tenantID)
	if err != nil {
		return fmt.Errorf("ensure tenant: %w", err)
	}
	return nil
}
