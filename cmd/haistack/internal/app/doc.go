// Package app wires haistack CLI commands to pkg/runtime and related services.
//
// This package is the shared bootstrap layer between cobra commands and the
// Health AI Stack libraries. It avoids duplicating pkg/runtime wiring inside
// each command handler.
//
// # Session lifecycle
//
// One-shot commands (validate, import, read, delete, export, search, sync,
// module lifecycle, config, audit, and reindex)
// call OpenSession, which builds a runtime.Runtime without starting HTTP,
// and Close on exit to shut down persistence backends.
//
// serve uses BuildRuntime directly with an HTTP listen address and manages
// Start/Shutdown in the command handler.
//
// BuildRuntime maps config.Config onto runtime.Builder selections: storage
// driver, search, module paths, sync hub URL/node ID, and optional HTTP.
//
// # Output
//
// Printer and OutputFormat implement the CLI output contract: text by default,
// JSON when --output json is set. Command handlers pass structured result
// values to Printer.Print for automation-friendly output.
//
// # Command helpers
//
//   - ReadResourceFile — parse one JSON file into types.ResourceEnvelope
//   - UpsertResource — create or update via core.ResourceService by id existence
//   - ParseSearchParams — parse repeated key=value CLI arguments
//   - NewReindexWorker — build a synchronous search.ReindexWorker for CLI reindex
//   - ReadSyncStatus / StatusReaderForSession — local sync status inspection
//   - FHIRPathValuesJSON — marshal FHIRPath result collections for eval output
//
// Sync status readers query SQLite or tenant-scoped Postgres tables directly
// for cursor position, pending sync.retry_push jobs, and unresolved conflicts.
package app
