package runtime

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// BlobStoreAdapter is the runtime seam for external object storage in cloud mode.
// Concrete provider implementations live outside pkg/runtime.
type BlobStoreAdapter interface {
	// Name identifies the adapter for logging and capability reporting.
	Name() string
	// BlobStore returns the blob persistence contract used by binary transfer flows.
	BlobStore() store.BlobStore
}

// ExternalSearchAdapter is the runtime seam for external search services in cloud mode.
type ExternalSearchAdapter interface {
	// Name identifies the adapter for logging and capability reporting.
	Name() string
	// SearchExecutor returns the query executor when search is delegated externally.
	SearchExecutor() store.SearchQueryExecutor
}

// WarehouseAdapter is the runtime seam for external analytics/reporting warehouses in cloud mode.
type WarehouseAdapter interface {
	// Name identifies the adapter for logging and capability reporting.
	Name() string
	// ReportingTables returns the reporting table contract for analytics refresh flows.
	ReportingTables() store.ReportingTableStore
}

// PostgresReportingWarehouse wraps a Postgres reporting table store for edge/cloud wiring.
type PostgresReportingWarehouse struct {
	Store store.ReportingTableStore
	Name_ string
}

func (w PostgresReportingWarehouse) Name() string {
	if w.Name_ != "" {
		return w.Name_
	}
	return "postgres-reporting"
}

func (w PostgresReportingWarehouse) ReportingTables() store.ReportingTableStore {
	return w.Store
}

// ParquetWarehouseAdapter routes analytics export rows to object storage as Parquet-compatible
// columnar JSON. It satisfies WarehouseAdapter for cloud mode seam tests.
type ParquetWarehouseAdapter struct {
	Name_ string
	Sink  func() store.ReportingTableStore
}

func (w ParquetWarehouseAdapter) Name() string {
	if w.Name_ != "" {
		return w.Name_
	}
	return "parquet-warehouse"
}

func (w ParquetWarehouseAdapter) ReportingTables() store.ReportingTableStore {
	if w.Sink != nil {
		return w.Sink()
	}
	return nil
}

// noopBlobStore is a minimal in-memory placeholder used only for adapter registration tests.
type noopBlobStore struct{}

func (noopBlobStore) Name() string { return "noop-blob" }

func (noopBlobStore) BlobStore() store.BlobStore { return nil }

// noopExternalSearch is a minimal placeholder for cloud seam tests.
type noopExternalSearch struct{}

func (noopExternalSearch) Name() string { return "noop-search" }

func (noopExternalSearch) SearchExecutor() store.SearchQueryExecutor { return nil }

// noopWarehouse is a minimal placeholder for cloud seam tests.
type noopWarehouse struct{}

func (noopWarehouse) Name() string { return "noop-warehouse" }

func (noopWarehouse) ReportingTables() store.ReportingTableStore { return nil }

// TestNoopBlobStore returns a minimal BlobStoreAdapter for seam tests.
func TestNoopBlobStore() BlobStoreAdapter { return noopBlobStore{} }

// TestNoopExternalSearch returns a minimal ExternalSearchAdapter for seam tests.
func TestNoopExternalSearch() ExternalSearchAdapter { return noopExternalSearch{} }

// TestNoopWarehouse returns a minimal WarehouseAdapter for seam tests.
func TestNoopWarehouse() WarehouseAdapter { return noopWarehouse{} }

// CloseableAdapter may be implemented by external adapters that hold connections.
type CloseableAdapter interface {
	Close(ctx context.Context) error
}
