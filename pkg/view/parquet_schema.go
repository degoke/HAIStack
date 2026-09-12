package view

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/parquet-go/parquet-go"
)

type parquetColumnKind int

const (
	parquetKindString parquetColumnKind = iota
	parquetKindBool
	parquetKindInt64
	parquetKindFloat64
)

// ParquetContentType is the SQL-on-FHIR native media type for parquet output.
const ParquetContentType = "application/vnd.apache.parquet"

// DefaultParquetPageSize is the default executor page size for streaming parquet export.
const DefaultParquetPageSize = 1000

// DefaultParquetRowGroupSize controls how many rows are buffered per parquet row group.
const DefaultParquetRowGroupSize = 1000

func buildParquetSchema(result *Result) (*parquet.Schema, []ColumnInfo, map[string]parquetColumnKind, error) {
	if result == nil {
		return nil, nil, nil, fmt.Errorf("view: nil result")
	}
	columns := append([]ColumnInfo(nil), result.Columns...)
	if len(columns) == 0 && len(result.Rows) > 0 {
		for name := range result.Rows[0] {
			columns = append(columns, ColumnInfo{Name: name})
		}
	}
	if len(columns) == 0 {
		return nil, nil, nil, fmt.Errorf("view: parquet export requires columns")
	}

	kinds := make(map[string]parquetColumnKind, len(columns))
	group := parquet.Group{}
	normalized := make([]ColumnInfo, len(columns))
	for i, col := range columns {
		fieldName := sanitizeParquetFieldName(col.Name)
		normalized[i] = ColumnInfo{Name: fieldName, Type: col.Type}
		kind := inferParquetColumnKind(col, result.Rows)
		kinds[fieldName] = kind
		group[fieldName] = optionalParquetNode(kind)
	}
	return parquet.NewSchema("view", group), normalized, kinds, nil
}

func optionalParquetNode(kind parquetColumnKind) parquet.Node {
	switch kind {
	case parquetKindBool:
		return parquet.Optional(parquet.Leaf(parquet.BooleanType))
	case parquetKindInt64:
		return parquet.Optional(parquet.Int(64))
	case parquetKindFloat64:
		return parquet.Optional(parquet.Leaf(parquet.DoubleType))
	default:
		return parquet.Optional(parquet.String())
	}
}

func inferParquetColumnKind(col ColumnInfo, rows []map[string]any) parquetColumnKind {
	switch strings.ToLower(strings.TrimSpace(col.Type)) {
	case "boolean", "bool":
		return parquetKindBool
	case "integer", "int", "long":
		return parquetKindInt64
	case "decimal", "number", "float", "double":
		return parquetKindFloat64
	case "string", "code", "id", "date", "datetime", "instant", "time", "uri", "markdown":
		return parquetKindString
	}
	for _, row := range rows {
		value, ok := rowValue(row, col.Name)
		if !ok || value == nil {
			continue
		}
		switch value.(type) {
		case bool:
			return parquetKindBool
		case int, int32, int64, uint, uint32, uint64:
			return parquetKindInt64
		case float32, float64:
			return parquetKindFloat64
		case []any, map[string]any:
			return parquetKindString
		default:
			return parquetKindString
		}
	}
	return parquetKindString
}

func sanitizeParquetFieldName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "column"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_', r == '-', r == '.':
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "column"
	}
	return b.String()
}

func rowValue(row map[string]any, column string) (any, bool) {
	if row == nil {
		return nil, false
	}
	if value, ok := row[column]; ok {
		return value, true
	}
	sanitized := sanitizeParquetFieldName(column)
	value, ok := row[sanitized]
	return value, ok
}

func prepareParquetRow(row map[string]any, columns []ColumnInfo, kinds map[string]parquetColumnKind) (map[string]any, error) {
	if row == nil {
		row = map[string]any{}
	}
	out := make(map[string]any, len(columns))
	for _, col := range columns {
		raw, ok := rowValue(row, col.Name)
		if !ok || raw == nil {
			continue
		}
		value, err := coerceParquetValue(raw, kinds[col.Name])
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", col.Name, err)
		}
		if value != nil {
			out[col.Name] = value
		}
	}
	return out, nil
}

func coerceParquetValue(raw any, kind parquetColumnKind) (any, error) {
	switch kind {
	case parquetKindBool:
		switch v := raw.(type) {
		case bool:
			return v, nil
		case string:
			switch strings.ToLower(v) {
			case "true":
				return true, nil
			case "false":
				return false, nil
			}
		}
	case parquetKindInt64:
		switch v := raw.(type) {
		case int:
			return int64(v), nil
		case int32:
			return int64(v), nil
		case int64:
			return v, nil
		case uint:
			return int64(v), nil
		case uint32:
			return int64(v), nil
		case uint64:
			return int64(v), nil
		case float64:
			return int64(v), nil
		}
	case parquetKindFloat64:
		switch v := raw.(type) {
		case float32:
			return float64(v), nil
		case float64:
			return v, nil
		case int:
			return float64(v), nil
		case int64:
			return float64(v), nil
		}
	}
	switch v := raw.(type) {
	case string:
		return v, nil
	case bool, int, int32, int64, uint, uint32, uint64, float32, float64:
		return fmt.Sprint(v), nil
	case []any, map[string]any:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return string(data), nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return string(data), nil
	}
}
