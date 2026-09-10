package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/degoke/health-ai-stack/pkg/view"
)

// ParquetFileSink writes a minimal columnar Parquet-compatible JSON bundle.
// v1 stores typed column metadata plus row arrays in a single JSON document
// with the .parquet extension so downstream pipelines can detect the sink
// without adding a heavyweight Parquet encoder dependency.
type ParquetFileSink struct {
	w io.Writer
}

// NewParquetFileSink returns a sink that writes columnar JSON to w.
func NewParquetFileSink(w io.Writer) *ParquetFileSink {
	return &ParquetFileSink{w: w}
}

type parquetDocument struct {
	Format  string           `json:"format"`
	Columns []view.ColumnInfo `json:"columns"`
	Rows    []map[string]any `json:"rows"`
}

// WriteRows encodes view rows as a columnar JSON document.
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
	doc := parquetDocument{
		Format:  "haistack-parquet-v1",
		Columns: append([]view.ColumnInfo(nil), result.Columns...),
		Rows:    append([]map[string]any(nil), result.Rows...),
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("encode parquet document: %w", err)
	}
	if _, err := s.w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write parquet document: %w", err)
	}
	return ctx.Err()
}

// NewParquetSink returns a ParquetFileSink-compatible RowSink.
func NewParquetSink(w io.Writer) RowSink {
	return NewParquetFileSink(w)
}
