package analytics

import (
	"context"
	"fmt"
	"io"

	"github.com/degoke/health-ai-stack/pkg/view"
)

// ParquetFileSink writes view rows as Apache Parquet binary.
type ParquetFileSink struct {
	w io.Writer
}

// NewParquetFileSink returns a sink that writes parquet to w.
func NewParquetFileSink(w io.Writer) *ParquetFileSink {
	return &ParquetFileSink{w: w}
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
	if err := view.WriteParquetResult(s.w, result); err != nil {
		return fmt.Errorf("write parquet: %w", err)
	}
	return ctx.Err()
}

// NewParquetSink returns a ParquetFileSink-compatible RowSink.
func NewParquetSink(w io.Writer) RowSink {
	return NewParquetFileSink(w)
}
