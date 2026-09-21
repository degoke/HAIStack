package parquetfhir

import (
	"testing"
	"time"

	"github.com/parquet-go/parquet-go/deprecated"
)

func TestTimestampToInt96UsesUnixMillis(t *testing.T) {
	start := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := timestampToInt96(start); got != deprecated.Int64ToInt96(0) {
		t.Fatalf("epoch start=%v, want 0 millis", got)
	}
	end := time.Date(1970, 1, 1, 23, 59, 59, 999000000, time.UTC)
	if got := timestampToInt96(end).Int64(); got != end.UnixMilli() {
		t.Fatalf("end millis=%d, want %d", got, end.UnixMilli())
	}
}

func TestApplyTimestampEncodingConvertsTimes(t *testing.T) {
	row := map[string]any{
		"birthDate":         "1970-01-01",
		"__birthDate_start": time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	out := applyTimestampEncoding(row, TimestampEncodingInt96)
	got, ok := out["__birthDate_start"].(deprecated.Int96)
	if !ok {
		t.Fatalf("type=%T, want deprecated.Int96", out["__birthDate_start"])
	}
	if got.Int64() != 0 {
		t.Fatalf("millis=%d, want 0", got.Int64())
	}
	unchanged := applyTimestampEncoding(map[string]any{
		"__birthDate_start": time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
	}, TimestampEncodingInt64)
	if _, ok := unchanged["__birthDate_start"].(time.Time); !ok {
		t.Fatalf("int64 path should keep time.Time, got %T", unchanged["__birthDate_start"])
	}
}
