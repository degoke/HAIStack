package view

import "github.com/degoke/health-ai-stack/pkg/parquetfhir"

// TimestampEncoding selects Parquet-on-FHIR date annotation physical types.
type TimestampEncoding = parquetfhir.TimestampEncoding

const (
	// TimestampEncodingInt64 writes TIMESTAMP(MILLIS) on INT64 (default).
	TimestampEncodingInt64 = parquetfhir.TimestampEncodingInt64
	// TimestampEncodingInt96 writes spec INT96 + TIMESTAMP(MILLIS).
	TimestampEncodingInt96 = parquetfhir.TimestampEncodingInt96
)

// ParseTimestampEncoding maps _parquetTimestampEncoding query or Parameters body values.
func ParseTimestampEncoding(raw string) (TimestampEncoding, error) {
	return parquetfhir.ParseTimestampEncoding(raw)
}

// NormalizeTimestampEncoding returns int64 unless enc is explicitly int96.
func NormalizeTimestampEncoding(enc TimestampEncoding) TimestampEncoding {
	return parquetfhir.NormalizeTimestampEncoding(enc)
}
