package parquetfhir

import (
	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/format"
)

// timestampMillisInt96Type is INT96 physical type with TIMESTAMP(MILLIS)
// logical type, as specified by Parquet-on-FHIR date range annotations.
//
// parquet-go's schemaElementTypeOf remaps TIMESTAMP to INT64 on read, so
// File.Schema and Pages cannot round-trip these columns. Use
// ReadInt96MillisColumn, which decodes from file metadata physical type.
type timestampMillisInt96Type struct {
	parquet.Type
	logical   *format.LogicalType
	converted *deprecated.ConvertedType
}

func newTimestampMillisInt96Type() parquet.Type {
	ts := parquet.Timestamp(parquet.Millisecond).Type()
	return timestampMillisInt96Type{
		Type:      parquet.Int96Type,
		logical:   ts.LogicalType(),
		converted: ts.ConvertedType(),
	}
}

func (t timestampMillisInt96Type) String() string {
	return "INT96 TIMESTAMP(isAdjustedToUTC=true, unit=MILLIS)"
}

func (t timestampMillisInt96Type) LogicalType() *format.LogicalType {
	return t.logical
}

func (t timestampMillisInt96Type) ConvertedType() *deprecated.ConvertedType {
	return t.converted
}
