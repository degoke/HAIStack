package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ReadReplica opens a read-only connection pool for analytics queries.
// Writes through this pool are not supported; use the primary DB for mutations.
func ReadReplica(ctx context.Context, dsn string, opts ...Option) (*DB, error) {
	return Open(ctx, dsn, opts...)
}

// WithReadPool attaches a read-only pool for read-scoped store accessors.
func (tdb *TenantDB) WithReadPool(readPool *pgxpool.Pool) *TenantDB {
	if tdb == nil || readPool == nil {
		return tdb
	}
	copy := *tdb
	copy.readPool = readPool
	return &copy
}

// ReadOnlyResourceStore returns a tenant-scoped resource store backed by the read
// replica pool when configured, otherwise the primary pool.
func (tdb *TenantDB) ReadOnlyResourceStore() *ResourceStore {
	if tdb.readPool != nil {
		return newResourceStore(tdb.readPool, tdb.tenantID)
	}
	return tdb.ResourceStore()
}

// PingReadReplica verifies connectivity to the read pool when configured.
func (tdb *TenantDB) PingReadReplica(ctx context.Context) error {
	pool := tdb.pool
	if tdb.readPool != nil {
		pool = tdb.readPool
	}
	if pool == nil {
		return fmt.Errorf("postgres: tenant db pool is nil")
	}
	return pool.Ping(ctx)
}
