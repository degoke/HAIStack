package analytics

import (
	"context"
	"fmt"
	"io"
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

// ManifestExportConfig configures cursor-based manifest export.
type ManifestExportConfig struct {
	Root          io.Writer
	Watermark     *WatermarkStore
	Format        ExportFormat
	ParquetLayout view.ParquetLayout
	Executor      *view.Executor
	Actor         string
}

type manifestExportSink struct {
	root          io.Writer
	watermark     *WatermarkStore
	format        ExportFormat
	parquetLayout view.ParquetLayout
	executor      *view.Executor
	actor         string
	mu            sync.Mutex
	lastRowCount  int
}

// NewManifestExportSink returns a sink that writes export payloads and advances watermarks.
func NewManifestExportSink(cfg ManifestExportConfig) ManifestExportSink {
	format := cfg.Format
	if format == "" {
		format = FormatNDJSON
	}
	layout := cfg.ParquetLayout
	if layout == "" {
		layout = view.ParquetLayoutFlat
	}
	return &manifestExportSink{
		root:          cfg.Root,
		watermark:     cfg.Watermark,
		format:        format,
		parquetLayout: layout,
		executor:      cfg.Executor,
		actor:         cfg.Actor,
	}
}

func (s *manifestExportSink) WriteRows(ctx context.Context, result *view.Result) error {
	if s == nil || s.root == nil {
		return fmt.Errorf("%w: manifest writer is required", ErrUnsupportedDestination)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRowCount = 0
	if err := s.writeFormatted(ctx, result); err != nil {
		return err
	}
	if s.watermark != nil && result != nil {
		return s.watermark.Advance(ctx, result.ViewName, result.Version, result.Metadata.ExecutedAt)
	}
	return nil
}

// LastExportRowCount implements ExportRowCountSink.
func (s *manifestExportSink) LastExportRowCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRowCount
}

func (s *manifestExportSink) writeFormatted(ctx context.Context, result *view.Result) error {
	switch s.format {
	case FormatCSV:
		return NewCSVSink(s.root).WriteRows(ctx, result)
	case FormatParquet:
		sink := NewParquetFileSinkWithConfig(ParquetFileSinkConfig{
			Writer:   s.root,
			Layout:   s.parquetLayout,
			Executor: s.executor,
			Actor:    s.actor,
		})
		if err := sink.WriteRows(ctx, result); err != nil {
			return err
		}
		s.lastRowCount = sink.LastExportRowCount()
		return nil
	default:
		return NewNDJSONSink(s.root).WriteRows(ctx, result)
	}
}
