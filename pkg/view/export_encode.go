package view

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func encodeRunResult(result *Result, format OutputFormat, header bool, layout ParquetLayout, exec *Executor, execReq ExecuteRequest) ([]byte, string, error) {
	switch format {
	case FormatCSV:
		var buf bytes.Buffer
		if err := writeCSV(&buf, result); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "text/csv", nil
	case FormatNDJSON:
		var buf bytes.Buffer
		for _, row := range result.Rows {
			b, err := json.Marshal(row)
			if err != nil {
				return nil, "", err
			}
			buf.Write(b)
			buf.WriteByte('\n')
		}
		return buf.Bytes(), "application/fhir+ndjson", nil
	case FormatParquet:
		var buf bytes.Buffer
		if layout == ParquetLayoutFHIR && exec != nil {
			if _, err := WriteParquetFHIRExport(context.Background(), &buf, exec, execReq); err != nil {
				return nil, "", err
			}
		} else if err := WriteParquetResult(&buf, result); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), ParquetContentType, nil
	default:
		if header {
			b, err := json.Marshal(result)
			return b, "application/json", err
		}
		b, err := json.Marshal(result.Rows)
		return b, "application/json", err
	}
}

func writeCSV(w interface{ Write([]byte) (int, error) }, result *Result) error {
	writer := csv.NewWriter(w)
	headers := make([]string, len(result.Columns))
	for i, col := range result.Columns {
		headers[i] = col.Name
	}
	if err := writer.Write(headers); err != nil {
		return err
	}
	for _, row := range result.Rows {
		record := make([]string, len(headers))
		for i, name := range headers {
			record[i] = encodeCSVCell(row[name])
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func encodeCSVCell(value any) string {
	if value == nil {
		return `\N`
	}
	switch v := value.(type) {
	case string:
		if strings.HasPrefix(v, `\`) {
			return `\` + v
		}
		return v
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	}
}

func writeFormattedExport(ctx context.Context, w interface{ Write([]byte) (int, error) }, result *Result, format OutputFormat) error {
	switch format {
	case FormatCSV:
		return writeCSV(w, result)
	case FormatParquet:
		return WriteParquetResult(w, result)
	default:
		for _, row := range result.Rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			b, err := json.Marshal(row)
			if err != nil {
				return err
			}
			if _, err := w.Write(b); err != nil {
				return err
			}
			if _, err := w.Write([]byte("\n")); err != nil {
				return err
			}
		}
		return ctx.Err()
	}
}
