// Command haistack is the Health AI Stack developer and operator CLI.
//
// haistack provides a runnable surface for local development, edge operations,
// and shell automation against a configured Health AI Stack runtime. It reads
// haistack.yaml (plus flag and environment overrides), wires pkg/runtime for
// server and one-shot commands, and prints human-readable or JSON output.
//
// # Build
//
//	go build -o bin/haistack ./cmd/haistack
//
// # Workspace bootstrap
//
//	haistack init
//
// Creates haistack.yaml with SQLite defaults and a .haistack/ data directory.
// Use --force to overwrite an existing config file.
//
// # Server
//
//	haistack serve
//
// Builds pkg/runtime from configuration, starts the managed HTTP server, prints
// the bound address, and blocks until SIGINT or SIGTERM.
//
// # Resource operations
//
//	haistack validate patient.json
//	haistack import patient.json
//
// validate runs the structural validation engine and exits non-zero when the
// resource is invalid. import upserts by resource type and id.
//
// # Search and FHIRPath
//
//	haistack search Patient name=Smith
//	haistack fhirpath eval patient.json 'Patient.name.family'
//
// search requires runtime.enableSearch. fhirpath eval prints the result
// collection as JSON (or formatted text by default).
//
// # Sync
//
//	haistack sync status
//	haistack sync push
//	haistack sync pull
//
// push and pull require sync.hubURL. status reads local cursor, job, and
// conflict stores only; it does not contact the remote hub.
//
// # Modules and indexing
//
//	haistack module install modules/core
//	haistack reindex Patient
//
// reindex runs synchronously in CLI phase 1 even though Postgres runtimes may
// enqueue background reindex jobs.
//
// # Configuration
//
// Default config file: haistack.yaml in the working directory (--config to
// override). Precedence: defaults, YAML file, HAISTACK_* environment variables,
// then command-line flags. Relative sqlitePath and modulePaths in a config file
// resolve against the config file directory.
//
// Use --output json on non-server commands for machine-readable results.
//
// See README.md in this directory for the full command reference, config schema,
// environment variable table, and package layout.
package main
