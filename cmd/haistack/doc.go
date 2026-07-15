// Command haistack is the Health AI Stack developer and operator CLI.
//
// Build:
//
//	go build -o bin/haistack ./cmd/haistack
//
// Initialize a workspace:
//
//	haistack init
//
// Start the local HTTP runtime:
//
//	haistack serve
//
// Validate and import one JSON resource:
//
//	haistack validate patient.json
//	haistack import patient.json
//
// Search and evaluate FHIRPath:
//
//	haistack search Patient name=Smith
//	haistack fhirpath eval patient.json 'Patient.name.family'
//
// Sync against a configured hub:
//
//	haistack sync status
//	haistack sync push
//	haistack sync pull
//
// Module install and search reindex:
//
//	haistack module install modules/core
//	haistack reindex Patient
//
// Configuration is read from haistack.yaml by default. Use --output json for
// structured command output. See README.md for flag and environment overrides.
package main
