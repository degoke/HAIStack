package parquetfhir

import (
	"bytes"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
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

func TestTimestampToInt96IsNotHiveJulianDay(t *testing.T) {
	epoch := timestampToInt96(time.Unix(0, 0).UTC())
	const hiveUnixEpochJulianDay uint32 = 2440588
	if epoch[2] == hiveUnixEpochJulianDay {
		t.Fatal("INT96 last word is Hive julian-day 2440588; layout is millis packed, not Hive nanos-of-day + julian day")
	}
	if epoch != deprecated.Int64ToInt96(0) {
		t.Fatalf("epoch INT96=%v, want millis-packed zero", epoch)
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

func TestReadInt96MillisColumnDataPageV1OptionalNulls(t *testing.T) {
	schema := parquet.NewSchema("row", parquet.Group{
		"ts": parquet.Optional(parquet.Leaf(newTimestampMillisInt96Type())),
	})
	var buf bytes.Buffer
	w := parquet.NewGenericWriter[map[string]any](&buf, schema, parquet.DataPageVersion(1))
	rows := []map[string]any{
		{"ts": timestampToInt96(time.UnixMilli(0).UTC())},
		{},
		{"ts": timestampToInt96(time.UnixMilli(1000).UTC())},
	}
	parquetRows := make([]parquet.Row, len(rows))
	for i, row := range rows {
		parquetRows[i] = schema.Deconstruct(nil, row)
	}
	if _, err := w.WriteRows(parquetRows); err != nil {
		t.Fatalf("WriteRows: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data := buf.Bytes()
	got, err := ReadInt96MillisColumn(bytes.NewReader(data), int64(len(data)), "ts")
	if err != nil {
		t.Fatalf("ReadInt96MillisColumn: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3 row-aligned values", len(got))
	}
	if got[0] == nil || !got[0].Equal(time.UnixMilli(0).UTC()) {
		t.Fatalf("row0=%v, want epoch", got[0])
	}
	if got[1] != nil {
		t.Fatalf("row1=%v, want nil", got[1])
	}
	if got[2] == nil || !got[2].Equal(time.UnixMilli(1000).UTC()) {
		t.Fatalf("row2=%v, want 1000ms", got[2])
	}
}
