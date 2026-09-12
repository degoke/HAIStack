// Package parquetfhir implements Parquet-on-FHIR nested resource layouts derived
// from FHIR StructureDefinitions (https://github.com/aehrc/parquet-on-fhir).
//
// Schemas are built from a base resource StructureDefinition and pruned to the
// union of fields present in the exported resource payloads. Each resource JSON
// object becomes one parquet row with nested groups and LIST columns for
// repeating elements.
package parquetfhir
