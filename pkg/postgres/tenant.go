package postgres

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TenantDB scopes all store operations to one tenant.
type TenantDB struct {
	pool     *pgxpool.Pool
	tenantID string
}

// TenantID returns the tenant identifier for this accessor.
func (tdb *TenantDB) TenantID() string {
	return tdb.tenantID
}

func (tdb *TenantDB) ensureTenant(ctx context.Context) error {
	_, err := tdb.pool.Exec(ctx, `
		INSERT INTO tenant (id) VALUES ($1)
		ON CONFLICT (id) DO NOTHING`, tdb.tenantID)
	if err != nil {
		return fmt.Errorf("ensure tenant %q: %w", tdb.tenantID, err)
	}
	return nil
}

// ResourceStore returns a tenant-scoped resource store.
func (tdb *TenantDB) ResourceStore() *ResourceStore {
	return newResourceStore(tdb.pool, tdb.tenantID)
}

// HistoryStore returns a tenant-scoped history store.
func (tdb *TenantDB) HistoryStore() *HistoryStore {
	return newHistoryStore(tdb.pool, tdb.tenantID)
}

// SearchStore returns a tenant-scoped search store.
func (tdb *TenantDB) SearchStore() *SearchStore {
	return newSearchStore(tdb.pool, tdb.tenantID)
}

// EventStore returns a tenant-scoped event store.
func (tdb *TenantDB) EventStore() *EventStore {
	return newEventStore(tdb.pool, tdb.tenantID)
}

// ConflictStore returns a tenant-scoped conflict store.
func (tdb *TenantDB) ConflictStore() *ConflictStore {
	return newConflictStore(tdb.pool, tdb.tenantID)
}

// CursorStore returns a tenant-scoped cursor store.
func (tdb *TenantDB) CursorStore() *CursorStore {
	return newCursorStore(tdb.pool, tdb.tenantID)
}

// IDRegistry returns a tenant-scoped ID registry store.
func (tdb *TenantDB) IDRegistry() *IDRegistry {
	return newIDRegistry(tdb.pool, tdb.tenantID)
}

// BinaryStore returns a tenant-scoped binary store.
func (tdb *TenantDB) BinaryStore() *BinaryStore {
	return newBinaryStore(tdb.pool, tdb.tenantID)
}

// BlobStore returns a tenant-scoped blob store.
func (tdb *TenantDB) BlobStore() *BlobStore {
	return newBlobStore(tdb.pool, tdb.tenantID)
}

// AuditStore returns a tenant-scoped audit store.
func (tdb *TenantDB) AuditStore() *AuditStore {
	return newAuditStore(tdb.pool, tdb.tenantID)
}

// ModuleStore returns a tenant-scoped module store.
func (tdb *TenantDB) ModuleStore() *ModuleStore {
	return newModuleStore(tdb.pool, tdb.tenantID)
}

// MaterializedViewStore returns a tenant-scoped materialized view store.
func (tdb *TenantDB) MaterializedViewStore() *MaterializedViewStore {
	return newMaterializedViewStore(tdb.pool, tdb.tenantID)
}

// AnalyticsStore returns a tenant-scoped analytics store.
func (tdb *TenantDB) AnalyticsStore() *AnalyticsStore {
	return newAnalyticsStore(tdb.pool, tdb.tenantID)
}

// JobStore returns a tenant-scoped job store.
func (tdb *TenantDB) JobStore() *JobStore {
	return newJobStore(tdb.pool, tdb.tenantID)
}

// NodeRegistry returns a tenant-scoped node registry store.
func (tdb *TenantDB) NodeRegistry() *NodeRegistry {
	return newNodeRegistry(tdb.pool, tdb.tenantID)
}

// InboxStore returns a tenant-scoped inbox idempotency store.
func (tdb *TenantDB) InboxStore() *InboxStore {
	return newInboxStore(tdb.pool, tdb.tenantID)
}

// RegistryInstallStore returns a tenant-scoped registry install overlay store.
func (tdb *TenantDB) RegistryInstallStore() *RegistryInstallStore {
	return newRegistryInstallStore(tdb.pool, tdb.tenantID)
}

// BeginWrite starts a tenant-scoped atomic write session.
func (tdb *TenantDB) BeginWrite(ctx context.Context) (store.WriteSession, error) {
	return tdb.BeginSession(ctx)
}

// BeginSession starts a tenant-scoped transaction session.
func (tdb *TenantDB) BeginSession(ctx context.Context) (*Session, error) {
	if err := tdb.ensureTenant(ctx); err != nil {
		return nil, err
	}
	tx, err := tdb.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin session: %w", err)
	}
	return newSession(tx, tdb.tenantID), nil
}

// ApplyWrite commits an accepted, rejected, or conflicted write atomically.
func (tdb *TenantDB) ApplyWrite(ctx context.Context, input Write) (*WriteResult, error) {
	session, err := tdb.BeginSession(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = session.Rollback(ctx)
		}
	}()

	result, err := session.applyWrite(ctx, input)
	if err != nil {
		return nil, err
	}
	if err := session.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return result, nil
}
