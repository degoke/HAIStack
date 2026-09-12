package view

import (
	"context"
	"fmt"
	"io"

	"github.com/parquet-go/parquet-go"
)

// ParquetRowWriter streams view rows into an Apache Parquet file.
type ParquetRowWriter struct {
	writer  *parquet.GenericWriter[map[string]any]
	columns []ColumnInfo
	kinds   map[string]parquetColumnKind
	rows    int
}

// NewParquetRowWriter opens a parquet writer using the schema from result metadata.
func NewParquetRowWriter(w io.Writer, result *Result) (*ParquetRowWriter, error) {
	schema, columns, kinds, err := buildParquetSchema(result)
	if err != nil {
		return nil, err
	}
	writer := parquet.NewGenericWriter[map[string]any](w, schema, parquet.MaxRowsPerRowGroup(DefaultParquetRowGroupSize))
	return &ParquetRowWriter{
		writer:  writer,
		columns: columns,
		kinds:   kinds,
	}, nil
}

// WriteRows appends one batch of rows to the parquet file.
func (pw *ParquetRowWriter) WriteRows(rows []map[string]any) error {
	if pw == nil || pw.writer == nil {
		return fmt.Errorf("view: parquet writer is not configured")
	}
	if len(rows) == 0 {
		return nil
	}
	batch := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		prepared, err := prepareParquetRow(row, pw.columns, pw.kinds)
		if err != nil {
			return err
		}
		batch = append(batch, prepared)
	}
	if _, err := pw.writer.Write(batch); err != nil {
		return fmt.Errorf("view: write parquet rows: %w", err)
	}
	pw.rows += len(batch)
	return nil
}

// RowCount returns the number of rows written so far.
func (pw *ParquetRowWriter) RowCount() int {
	if pw == nil {
		return 0
	}
	return pw.rows
}

// Close flushes the parquet footer.
func (pw *ParquetRowWriter) Close() error {
	if pw == nil || pw.writer == nil {
		return nil
	}
	return pw.writer.Close()
}

// WriteParquetResult writes a complete view result as one parquet file.
func WriteParquetResult(w io.Writer, result *Result) error {
	writer, err := NewParquetRowWriter(w, result)
	if err != nil {
		return err
	}
	if err := writer.WriteRows(result.Rows); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

// IsParquetFile reports whether data begins with the parquet magic bytes.
func IsParquetFile(data []byte) bool {
	return len(data) >= 4 && string(data[:4]) == "PAR1"
}

// ParquetFileRowCount returns the number of rows in a parquet file payload.
func ParquetFileRowCount(data []byte) (int64, error) {
	file, err := parquet.OpenFile(bytesReader(data), int64(len(data)))
	if err != nil {
		return 0, err
	}
	return file.NumRows(), nil
}

type bytesReader []byte

func (b bytesReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	return copy(p, b[off:]), nil
}

// WriteParquetExport streams a view export through paginated executor calls.
func WriteParquetExport(ctx context.Context, w io.Writer, exec *Executor, req ExecuteRequest, pageSize int) (int, error) {
	if exec == nil {
		return 0, fmt.Errorf("view: executor is required")
	}
	if pageSize <= 0 {
		pageSize = DefaultParquetPageSize
	}

	var writer *ParquetRowWriter
	totalRows := 0
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	for {
		if err := ctx.Err(); err != nil {
			if writer != nil {
				_ = writer.Close()
			}
			return totalRows, err
		}
		pageReq := req
		pageReq.Limit = pageSize
		pageReq.Offset = offset
		result, err := exec.Execute(ctx, pageReq)
		if err != nil {
			if writer != nil {
				_ = writer.Close()
			}
			return totalRows, err
		}
		if writer == nil {
			writer, err = NewParquetRowWriter(w, result)
			if err != nil {
				return totalRows, err
			}
		}
		if err := writer.WriteRows(result.Rows); err != nil {
			_ = writer.Close()
			return totalRows, err
		}
		totalRows += len(result.Rows)
		if result.NextOffset == nil {
			break
		}
		offset = *result.NextOffset
	}
	if writer == nil {
		// Empty export still produces a valid parquet file with schema from view metadata.
		spec, err := exec.ResolveView(req.ViewName, req.Version)
		if err != nil {
			return 0, err
		}
		empty := &Result{
			ViewName: req.ViewName,
			Version:  req.Version,
			Columns:  spec.ColumnInfos(),
			Rows:     nil,
		}
		writer, err = NewParquetRowWriter(w, empty)
		if err != nil {
			return 0, err
		}
	}
	if err := writer.Close(); err != nil {
		return totalRows, err
	}
	return totalRows, nil
}
