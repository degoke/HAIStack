package analytics

import (
	"context"
	"fmt"
	"io"

	"github.com/degoke/health-ai-stack/pkg/view"
)

// ParquetFileSink writes view rows as Apache Parquet binary.
type ParquetFileSink struct {
	w            io.Writer
	layout       view.ParquetLayout
	executor     *view.Executor
	actor        string
	lastRowCount int
}

// ParquetFileSinkConfig configures ParquetFileSink encoding.
type ParquetFileSinkConfig struct {
	Writer   io.Writer
	Layout   view.ParquetLayout
	Executor *view.Executor
	Actor    string
}

// NewParquetFileSink returns a sink that writes flat-view parquet to w.
func NewParquetFileSink(w io.Writer) *ParquetFileSink {
	return &ParquetFileSink{w: w, layout: view.ParquetLayoutFlat}
}

// NewParquetFileSinkWithConfig returns a sink with optional Parquet-on-FHIR layout.
func NewParquetFileSinkWithConfig(cfg ParquetFileSinkConfig) *ParquetFileSink {
	layout := cfg.Layout
	if layout == "" {
		layout = view.ParquetLayoutFlat
	}
	return &ParquetFileSink{
		w:        cfg.Writer,
		layout:   layout,
		executor: cfg.Executor,
		actor:    cfg.Actor,
	}
}

// WriteRows encodes view rows as Apache Parquet.
func (s *ParquetFileSink) WriteRows(ctx context.Context, result *view.Result) error {
	if s == nil || s.w == nil {
		return fmt.Errorf("%w: parquet writer is required", ErrUnsupportedDestination)
	}
	if result == nil {
		return fmt.Errorf("analytics: nil view result")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rowCount, err := writeParquet(ctx, s.w, result, s.layout, s.executor, s.actor)
	if err != nil {
		return fmt.Errorf("write parquet: %w", err)
	}
	s.lastRowCount = rowCount
	return ctx.Err()
}

// LastExportRowCount implements ExportRowCountSink.
func (s *ParquetFileSink) LastExportRowCount() int {
	if s == nil {
		return 0
	}
	return s.lastRowCount
}

// NewParquetSink returns a ParquetFileSink-compatible RowSink.
func NewParquetSink(w io.Writer) RowSink {
	return NewParquetFileSink(w)
}
