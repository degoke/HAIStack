// Package parquetfhir implements Parquet-on-FHIR nested resource layouts derived
// from FHIR StructureDefinitions (https://github.com/aehrc/parquet-on-fhir).
//
// Schemas are built from a base resource StructureDefinition and pruned to the
// union of fields present in exported resource payloads. The encoder adds spec
// annotations for date/dateTime ranges, decimal numerics, and Quantity canonical
// groups (UCUM Cel/[degF]/K → Kelvin). Contained resources are merged into the
// contained LIST schema. Timestamp annotations use Parquet TIMESTAMP(MILLIS) on
// INT64 (logical equivalent to spec INT96 + TIMESTAMP MILLIS; INT96 is avoided
// because parquet-go map writers cannot encode deprecated Int96 arrays). Use
// _parquetLayout=fhir on view export/run operations and analytics lakehouse sinks.
package parquetfhir
