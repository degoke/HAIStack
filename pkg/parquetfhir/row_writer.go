package parquetfhir

import (
	"fmt"
	"io"

	"github.com/parquet-go/parquet-go"
)

type resourceWriter interface {
	Write(rows []map[string]any) error
	Close() error
}

func newResourceWriter(w io.Writer, schema *parquet.Schema, cfg writeConfig) resourceWriter {
	return &genericResourceWriter{
		w:      parquet.NewGenericWriter[map[string]any](w, schema, parquet.MaxRowsPerRowGroup(int64(cfg.rowGroupSize))),
		schema: schema,
		int96:  cfg.timestampEncoding == TimestampEncodingInt96,
	}
}

// genericResourceWriter uses parquet-go's GenericWriter. The INT64 path writes
// maps directly. INT96 uses WriteRows after Deconstruct because map Write()
// cannot encode deprecated INT96 values.
type genericResourceWriter struct {
	w      *parquet.GenericWriter[map[string]any]
	schema *parquet.Schema
	int96  bool
}

func (g *genericResourceWriter) Write(rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	if !g.int96 {
		_, err := g.w.Write(rows)
		return err
	}
	parquetRows := make([]parquet.Row, len(rows))
	for i, row := range rows {
		decoded, err := deconstructRow(g.schema, row)
		if err != nil {
			return err
		}
		parquetRows[i] = decoded
	}
	_, err := g.w.WriteRows(parquetRows)
	return err
}

func (g *genericResourceWriter) Close() error {
	if g == nil || g.w == nil {
		return nil
	}
	return g.w.Close()
}

func deconstructRow(schema *parquet.Schema, row map[string]any) (decoded parquet.Row, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("parquetfhir: deconstruct INT96 row: %v", rec)
		}
	}()
	return schema.Deconstruct(nil, row), nil
}
