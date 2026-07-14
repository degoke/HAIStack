package analytics

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/degoke/health-ai-stack/pkg/view"
)

// CSVSink writes view rows as CSV with deterministic column ordering from the
// view schema. Null values are empty fields; arrays and complex values are JSON.
type CSVSink struct {
	w io.Writer
}

// NewCSVSink returns a sink that writes CSV to w.
func NewCSVSink(w io.Writer) *CSVSink {
	return &CSVSink{w: w}
}

// WriteRows encodes result as CSV.
func (s *CSVSink) WriteRows(_ context.Context, result *view.Result) error {
	if s == nil || s.w == nil {
		return fmt.Errorf("%w: csv writer is required", ErrUnsupportedDestination)
	}
	if result == nil {
		return fmt.Errorf("analytics: nil view result")
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	headers := columnNames(result.Columns)
	if err := writer.Write(headers); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}

	for _, row := range result.Rows {
		record := make([]string, len(headers))
		for i, name := range headers {
			record[i] = encodeCSVCell(row[name])
		}
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("write csv row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush csv: %w", err)
	}

	if _, err := s.w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write csv output: %w", err)
	}
	return nil
}

func columnNames(columns []view.ColumnInfo) []string {
	names := make([]string, len(columns))
	for i, col := range columns {
		names[i] = col.Name
	}
	return names
}

func encodeCSVCell(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 64)
	case []any:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	case map[string]any:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	}
}
