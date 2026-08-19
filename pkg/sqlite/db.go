package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
	_ "modernc.org/sqlite"
)

const driverName = "sqlite"

// DB owns the shared SQLite connection, migration runner, and store constructors.
type DB struct {
	sql *sql.DB
}

// Open opens a SQLite database at path with default pragmas and wiring.
func Open(path string, opts ...Option) (*DB, error) {
	cfg := defaultOptions()
	for _, opt := range opts {
		opt(&cfg)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)",
		path, cfg.busyTimeout.Milliseconds())

	sqlDB, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	return &DB{sql: sqlDB}, nil
}

// Migrate runs embedded numbered SQL migrations.
func (db *DB) Migrate(ctx context.Context) error {
	return runMigrations(ctx, db.sql)
}

// OpenAndMigrate opens a SQLite database and runs embedded migrations.
func OpenAndMigrate(ctx context.Context, path string, opts ...Option) (*DB, error) {
	db, err := Open(path, opts...)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	if db.sql == nil {
		return nil
	}
	return db.sql.Close()
}

// SQL returns the underlying database handle for advanced use.
func (db *DB) SQL() *sql.DB {
	return db.sql
}

// ResourceStore returns a connection-scoped resource store.
func (db *DB) ResourceStore() *ResourceStore {
	return newResourceStore(db.sql)
}

// HistoryStore returns a connection-scoped history store.
func (db *DB) HistoryStore() *HistoryStore {
	return newHistoryStore(db.sql)
}

// SearchStore returns a connection-scoped search store.
func (db *DB) SearchStore() *SearchStore {
	return newSearchStore(db.sql)
}

// OutboxStore returns a connection-scoped outbox event store.
func (db *DB) OutboxStore() *OutboxStore {
	return newOutboxStore(db.sql)
}

// InboxStore returns a connection-scoped inbox applied store.
func (db *DB) InboxStore() *InboxStore {
	return newInboxStore(db.sql)
}

// CursorStore returns a connection-scoped cursor store.
func (db *DB) CursorStore() *CursorStore {
	return newCursorStore(db.sql)
}

// ConflictStore returns a connection-scoped conflict store.
func (db *DB) ConflictStore() *ConflictStore {
	return newConflictStore(db.sql)
}

// BinaryStore returns a connection-scoped inline binary store.
func (db *DB) BinaryStore() *BinaryStore {
	return newBinaryStore(db.sql)
}

// BlobMetadataStore returns a connection-scoped blob metadata store.
func (db *DB) BlobMetadataStore() *BlobMetadataStore {
	return newBlobMetadataStore(db.sql)
}

// SQLiteBlobStore returns a connection-scoped chunked blob store.
func (db *DB) SQLiteBlobStore() *SQLiteBlobStore {
	return newSQLiteBlobStore(db.sql)
}

// ChunkBlobStore returns a connection-scoped chunked blob store.
func (db *DB) ChunkBlobStore() *ChunkBlobStore {
	return db.SQLiteBlobStore()
}

// ModuleStore returns a connection-scoped module registry store.
func (db *DB) ModuleStore() *ModuleStore {
	return newModuleStore(db.sql)
}

// DefinitionStore returns a connection-scoped FHIR definition catalog store.
func (db *DB) DefinitionStore() *DefinitionStore {
	return newDefinitionStore(db.sql)
}
func (db *DB) TerminologyStore() *TerminologyStore { return newTerminologyStore(db.sql, "default") }

// RegistryInstallStore returns a connection-scoped registry install overlay store.
func (db *DB) RegistryInstallStore() *RegistryInstallStore {
	return newRegistryInstallStore(db.sql)
}

// AuthStore returns a tenant-scoped auth persistence adapter.
func (db *DB) AuthStore(tenantID string) *AuthStore {
	return newAuthStore(db.sql, tenantID)
}

// JobStore returns a connection-scoped background job store.
func (db *DB) JobStore() *JobStore {
	return newJobStore(db.sql)
}

// AuditStore returns a connection-scoped audit store.
func (db *DB) AuditStore() *AuditStore {
	return newAuditStore(db.sql)
}

// SubscriptionStore returns a connection-scoped subscription registry store.
func (db *DB) SubscriptionStore() *SubscriptionStore {
	return newSubscriptionStore(db.sql)
}

// SubscriptionDeliveryStore returns a connection-scoped delivery log store.
func (db *DB) SubscriptionDeliveryStore() *SubscriptionDeliveryStore {
	return newSubscriptionDeliveryStore(db.sql)
}

// LocalWrite bundles the inputs for one atomic local write pipeline.
type LocalWrite struct {
	Resource      *types.ResourceEnvelope
	Action        store.VersionAction
	SearchEntries []store.SearchIndexEntry
	Event         store.ResourceEvent
	Version       store.ResourceVersion
}

// LocalWriteResult returns persisted write outputs.
type LocalWriteResult struct {
	Version store.ResourceVersion
	Event   store.ResourceEvent
}

// BeginWrite starts a new atomic write session.
func (db *DB) BeginWrite(ctx context.Context) (store.WriteSession, error) {
	return db.BeginSession(ctx)
}

// ApplyLocalWrite commits resource, history, outbox, and search changes atomically.
func (db *DB) ApplyLocalWrite(ctx context.Context, input LocalWrite) (*LocalWriteResult, error) {
	session, err := db.BeginSession(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = session.Rollback(ctx)
		}
	}()

	result, err := session.applyLocalWrite(ctx, input)
	if err != nil {
		return nil, err
	}
	if err := session.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return result, nil
}
