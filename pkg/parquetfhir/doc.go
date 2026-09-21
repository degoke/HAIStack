// Package parquetfhir implements Parquet-on-FHIR nested resource layouts derived
// from FHIR StructureDefinitions (https://github.com/aehrc/parquet-on-fhir).
//
// Schemas are built from a base resource StructureDefinition and pruned to the
// union of fields present in exported resource payloads. The encoder adds spec
// annotations for date/dateTime ranges, decimal numerics, and Quantity canonical
// groups (UCUM temperature, length, and mass). Contained resources are merged into
// the contained LIST schema. Profile URLs in meta.profile merge additional
// StructureDefinition elements into the schema index. Timestamp annotations default
// to Parquet TIMESTAMP(MILLIS) on INT64 for parquet-go map writer compatibility.
// Pass WithTimestampEncoding(TimestampEncodingInt96) or set
// _parquetTimestampEncoding=int96 (query or Parameters body) to emit spec
// INT96 + TIMESTAMP(MILLIS) via a typed parquet.Row writer. Values are Unix
// millis packed with Int64ToInt96. parquet-go remaps TIMESTAMP to INT64 on
// read; use ReadInt96MillisColumn to round-trip those columns (PLAIN INT96
// workaround; optional columns are row-aligned, LIST columns are
// definition-level aligned). WriteResourcesStreaming uses a
// two-pass replay model for bounded memory. Use _parquetLayout=fhir on view
// export/run operations and analytics lakehouse sinks.
package parquetfhir
