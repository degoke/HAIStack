package analytics

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/view"
)

// RowSink writes structured view rows to an export-oriented destination.
type RowSink interface {
	WriteRows(ctx context.Context, result *view.Result) error
}

// ParquetSink writes view rows as columnar JSON documents.
type ParquetSink interface {
	WriteRows(ctx context.Context, result *view.Result) error
}

// WarehouseSink writes view rows to a warehouse backend.
type WarehouseSink interface {
	WriteRows(ctx context.Context, result *view.Result) error
}

// LakehouseSink writes view rows to a data lake / lakehouse.
type LakehouseSink interface {
	WriteRows(ctx context.Context, result *view.Result) error
}

// ManifestExportSink supports cursor-based incremental export with manifests.
type ManifestExportSink interface {
	WriteRows(ctx context.Context, result *view.Result) error
}
