// Package parquetfhir implements Parquet-on-FHIR nested resource layouts derived
// from FHIR StructureDefinitions (https://github.com/aehrc/parquet-on-fhir).
//
// Schemas are built from a base resource StructureDefinition and pruned to the
// union of fields present in exported resource payloads. The encoder adds spec
// annotations for date/dateTime ranges, decimal numerics, and Quantity canonical
// groups. Use _parquetLayout=fhir on view export/run operations.
package parquetfhir
