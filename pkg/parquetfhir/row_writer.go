package parquetfhir

import (
	"io"

	"github.com/parquet-go/parquet-go"
)

type resourceWriter interface {
	Write(rows []map[string]any) error
	Close() error
}

func newResourceWriter(w io.Writer, schema *parquet.Schema, cfg writeConfig) resourceWriter {
	if cfg.timestampEncoding == TimestampEncodingInt96 {
		return newTypedRowWriter(w, schema, cfg.rowGroupSize)
	}
	return newMapResourceWriter(w, schema, cfg.rowGroupSize)
}

type mapResourceWriter struct {
	w *parquet.GenericWriter[map[string]any]
}

func newMapResourceWriter(w io.Writer, schema *parquet.Schema, rowGroupSize int) *mapResourceWriter {
	return &mapResourceWriter{
		w: parquet.NewGenericWriter[map[string]any](w, schema, parquet.MaxRowsPerRowGroup(int64(rowGroupSize))),
	}
}

func (m *mapResourceWriter) Write(rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	_, err := m.w.Write(rows)
	return err
}

func (m *mapResourceWriter) Close() error {
	if m == nil || m.w == nil {
		return nil
	}
	return m.w.Close()
}

// typedRowWriter writes parquet.Row values so INT96 annotation columns can be
// encoded. parquet-go map GenericWriter cannot emit deprecated INT96 arrays.
type typedRowWriter struct {
	w      *parquet.Writer
	schema *parquet.Schema
}

func newTypedRowWriter(w io.Writer, schema *parquet.Schema, rowGroupSize int) *typedRowWriter {
	return &typedRowWriter{
		w:      parquet.NewWriter(w, schema, parquet.MaxRowsPerRowGroup(int64(rowGroupSize))),
		schema: schema,
	}
}

func (t *typedRowWriter) Write(rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	parquetRows := make([]parquet.Row, len(rows))
	for i, row := range rows {
		parquetRows[i] = t.schema.Deconstruct(nil, row)
	}
	_, err := t.w.WriteRows(parquetRows)
	return err
}

func (t *typedRowWriter) Close() error {
	if t == nil || t.w == nil {
		return nil
	}
	return t.w.Close()
}
