package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB owns the shared Postgres connection pool, migration runner, and tenant accessors.
type DB struct {
	pool   *pgxpool.Pool
	schema dbSchema
}

// Open opens a Postgres connection pool at dsn with default pool settings.
func Open(ctx context.Context, dsn string, opts ...Option) (*DB, error) {
	cfg := defaultOptions()
	for _, opt := range opts {
		opt(&cfg)
	}

	schemaName, err := normalizeSchema(cfg.schema)
	if err != nil {
		return nil, err
	}
	schema := dbSchema{name: schemaName}

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	poolConfig.MaxConns = cfg.maxConns
	poolConfig.MinConns = cfg.minConns
	poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schema.searchPath())
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &DB{pool: pool, schema: schema}, nil
}

// Schema returns the configured Postgres schema for haistack tables.
func (db *DB) Schema() string {
	return db.schema.name
}

// Migrate runs embedded numbered SQL migrations.
func (db *DB) Migrate(ctx context.Context) error {
	return runMigrations(ctx, db.pool, db.schema)
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
		INSERT INTO hai_tenant (id) VALUES ($1)
		ON CONFLICT (id) DO NOTHING`, tenantID)
	if err != nil {
		return fmt.Errorf("ensure tenant: %w", err)
	}
	return nil
}
