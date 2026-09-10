package analytics

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/view"
)

// warehouseSink writes view rows into ReportingTableStore.
type warehouseSink struct {
	target *ReportingTarget
}

// NewWarehouseSink returns a sink that refreshes reporting tables.
func NewWarehouseSink(store store.ReportingTableStore) WarehouseSink {
	return &warehouseSink{target: NewReportingTarget(store)}
}

func (s *warehouseSink) WriteRows(ctx context.Context, result *view.Result) error {
	if s == nil || s.target == nil {
		return fmt.Errorf("%w: reporting store is required", ErrUnsupportedDestination)
	}
	return s.target.Write(ctx, result)
}

// LakehouseConfig configures partitioned lakehouse export.
type LakehouseConfig struct {
	Root        io.Writer
	PartitionBy func(*view.Result) string
}

type lakehouseSink struct {
	root        io.Writer
	partitionBy func(*view.Result) string
	mu          sync.Mutex
}

// NewLakehouseSink returns a sink that writes partitioned parquet-compatible JSON.
func NewLakehouseSink(cfg LakehouseConfig) LakehouseSink {
	partitionBy := cfg.PartitionBy
	if partitionBy == nil {
		partitionBy = defaultLakehousePartition
	}
	return &lakehouseSink{
		root:        cfg.Root,
		partitionBy: partitionBy,
	}
}

func defaultLakehousePartition(result *view.Result) string {
	if result == nil {
		return "view=unknown"
	}
	return path.Join("view="+sanitizePartition(result.ViewName), "version="+sanitizePartition(result.Version))
}

func sanitizePartition(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "/", "_")
	if value == "" {
		return "unknown"
	}
	return value
}

func (s *lakehouseSink) WriteRows(ctx context.Context, result *view.Result) error {
	if s == nil || s.root == nil {
		return fmt.Errorf("%w: lakehouse writer is required", ErrUnsupportedDestination)
	}
	partition := s.partitionBy(result)
	header := []byte("{\"partition\":\"" + partition + "\",\"format\":\"haistack-lakehouse-v1\"}\n")
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.root.Write(header); err != nil {
		return fmt.Errorf("write lakehouse partition header: %w", err)
	}
	return NewParquetSink(s.root).WriteRows(ctx, result)
}

// ManifestExportConfig configures cursor-based manifest export.
type ManifestExportConfig struct {
	Root      io.Writer
	Watermark *WatermarkStore
	Format    ExportFormat
}

type manifestExportSink struct {
	root      io.Writer
	watermark *WatermarkStore
	format    ExportFormat
	mu        sync.Mutex
}

// NewManifestExportSink returns a sink that writes export payloads and advances watermarks.
func NewManifestExportSink(cfg ManifestExportConfig) ManifestExportSink {
	format := cfg.Format
	if format == "" {
		format = FormatNDJSON
	}
	return &manifestExportSink{
		root:      cfg.Root,
		watermark: cfg.Watermark,
		format:    format,
	}
}

func (s *manifestExportSink) WriteRows(ctx context.Context, result *view.Result) error {
	if s == nil || s.root == nil {
		return fmt.Errorf("%w: manifest writer is required", ErrUnsupportedDestination)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeFormatted(ctx, result); err != nil {
		return err
	}
	if s.watermark != nil && result != nil {
		return s.watermark.Advance(ctx, result.ViewName, result.Version, result.Metadata.ExecutedAt)
	}
	return nil
}

func (s *manifestExportSink) writeFormatted(ctx context.Context, result *view.Result) error {
	switch s.format {
	case FormatCSV:
		return NewCSVSink(s.root).WriteRows(ctx, result)
	case FormatParquet:
		return NewParquetSink(s.root).WriteRows(ctx, result)
	default:
		return NewNDJSONSink(s.root).WriteRows(ctx, result)
	}
}
