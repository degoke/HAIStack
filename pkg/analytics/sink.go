package analytics

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/view"
)

// RowSink writes structured view rows to an export-oriented destination.
type RowSink interface {
	WriteRows(ctx context.Context, result *view.Result) error
}

// ParquetSink writes view rows as Parquet files. Not implemented in v1.
type ParquetSink interface {
	WriteRows(ctx context.Context, result *view.Result) error
}

// WarehouseSink writes view rows to a warehouse backend. Not implemented in v1.
type WarehouseSink interface {
	WriteRows(ctx context.Context, result *view.Result) error
}

// LakehouseSink writes view rows to a data lake / lakehouse. Not implemented in v1.
type LakehouseSink interface {
	WriteRows(ctx context.Context, result *view.Result) error
}

// ManifestExportSink supports cursor-based incremental export with manifests.
// Not implemented in v1.
type ManifestExportSink interface {
	WriteRows(ctx context.Context, result *view.Result) error
}

// deferredSink is a placeholder for unimplemented sink backends.
type deferredSink struct {
	name string
}

func (d *deferredSink) WriteRows(context.Context, *view.Result) error {
	return ErrSinkNotImplemented
}

// NewParquetSink returns a stub Parquet sink for interface wiring tests.
func NewParquetSink() ParquetSink {
	return &deferredSink{name: "parquet"}
}

// NewWarehouseSink returns a stub warehouse sink for interface wiring tests.
func NewWarehouseSink() WarehouseSink {
	return &deferredSink{name: "warehouse"}
}

// NewLakehouseSink returns a stub lakehouse sink for interface wiring tests.
func NewLakehouseSink() LakehouseSink {
	return &deferredSink{name: "lakehouse"}
}

// NewManifestExportSink returns a stub manifest export sink for interface wiring tests.
func NewManifestExportSink() ManifestExportSink {
	return &deferredSink{name: "manifest"}
}
