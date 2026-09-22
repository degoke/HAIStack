package parquetfhir

import (
	"fmt"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go/deprecated"
)

// TimestampEncoding selects the physical type used for date-range annotation
// columns (__field_start / __field_end).
type TimestampEncoding string

const (
	// TimestampEncodingInt64 writes TIMESTAMP(MILLIS) on INT64 (default).
	// Compatible with parquet-go map writers and modern query engines.
	TimestampEncodingInt64 TimestampEncoding = "int64"
	// TimestampEncodingInt96 writes TIMESTAMP(MILLIS) on deprecated INT96,
	// matching the Parquet-on-FHIR spec physical type.
	TimestampEncodingInt96 TimestampEncoding = "int96"
)

// ParseTimestampEncoding maps _parquetTimestampEncoding query or Parameters
// body values. Empty input defaults to int64. Unknown values return an error.
func ParseTimestampEncoding(raw string) (TimestampEncoding, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "int64":
		return TimestampEncodingInt64, nil
	case "int96":
		return TimestampEncodingInt96, nil
	default:
		return "", fmt.Errorf("parquetfhir: invalid timestamp encoding %q (want int64 or int96)", raw)
	}
}

// NormalizeTimestampEncoding returns int64 unless enc is explicitly int96.
func NormalizeTimestampEncoding(enc TimestampEncoding) TimestampEncoding {
	if enc == TimestampEncodingInt96 {
		return TimestampEncodingInt96
	}
	return TimestampEncodingInt64
}

// timestampToInt96 packs Unix milliseconds into parquet INT96 via Int64ToInt96.
// This matches TIMESTAMP(MILLIS) and is not the Hive/Impala INT96 layout
// (8-byte nanoseconds-of-day + 4-byte Julian day). Engines that ignore the
// logical type and treat every INT96 as a Hive timestamp will misread values.
func timestampToInt96(t time.Time) deprecated.Int96 {
	return deprecated.Int64ToInt96(t.UTC().UnixMilli())
}

func applyTimestampEncoding(row map[string]any, enc TimestampEncoding) map[string]any {
	if NormalizeTimestampEncoding(enc) != TimestampEncodingInt96 || row == nil {
		return row
	}
	encodeTimestampValues(row)
	return row
}

func encodeTimestampValues(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			switch typed := item.(type) {
			case time.Time:
				v[key] = timestampToInt96(typed)
			case map[string]any, []any:
				encodeTimestampValues(typed)
			}
		}
	case []any:
		for i, item := range v {
			switch typed := item.(type) {
			case time.Time:
				v[i] = timestampToInt96(typed)
			case map[string]any, []any:
				encodeTimestampValues(typed)
			}
		}
	}
}
