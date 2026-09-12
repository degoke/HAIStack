// Package parquetfhir implements Parquet-on-FHIR nested resource layouts derived
// from FHIR StructureDefinitions (https://github.com/aehrc/parquet-on-fhir).
//
// Schemas are built from a base resource StructureDefinition and pruned to the
// union of fields present in exported resource payloads. The encoder adds spec
// annotations for date/dateTime ranges, decimal numerics, and Quantity canonical
// groups (UCUM temperature, length, and mass). Contained resources are merged into
// the contained LIST schema. Profile URLs in meta.profile merge additional
// StructureDefinition elements into the schema index. Timestamp annotations use
// Parquet TIMESTAMP(MILLIS) on INT64 (logical equivalent to spec INT96 +
// TIMESTAMP MILLIS; INT96 is avoided because parquet-go map writers cannot encode
// deprecated Int96 arrays). WriteResourcesStreaming uses a two-pass replay model
// for bounded memory. Use _parquetLayout=fhir on view export/run operations and
// analytics lakehouse sinks.
package parquetfhir
