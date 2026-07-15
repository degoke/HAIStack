// Package config defines the haistack CLI configuration schema and loading rules.
//
// Configuration is YAML-first with layered overrides. The Load function merges
// built-in defaults, an optional config file, HAISTACK_* environment variables,
// and command-line Overrides (supplied by the cobra root command).
//
// # Schema
//
// Top-level sections:
//
//   - storage — driver (sqlite or postgres), paths, DSN, tenant ID
//   - runtime — HTTP address, search enablement, module install paths
//   - sync — hub URL and device node ID
//
// Defaults target a local SQLite workspace at .haistack/haistack.db with
// modules/core installed and search enabled.
//
// # File resolution
//
// When a config file is present, relative storage.sqlitePath and
// runtime.modulePaths values are resolved against the directory containing
// the config file. This allows haistack.yaml to live at the repo root while
// referencing .haistack/haistack.db and modules/core as sibling paths.
//
// If the default haistack.yaml is missing, Load falls back to built-in
// defaults so commands can run before haistack init.
//
// # Validation
//
// Validate enforces driver-specific requirements: sqlitePath for SQLite,
// postgresDSN and tenantID for Postgres, and sync.nodeID when sync.hubURL
// is set.
//
// # Environment variables
//
//   - HAISTACK_STORAGE_DRIVER
//   - HAISTACK_SQLITE_PATH
//   - HAISTACK_POSTGRES_DSN
//   - HAISTACK_TENANT_ID
//   - HAISTACK_HTTP_ADDR
//   - HAISTACK_ENABLE_SEARCH
//   - HAISTACK_MODULE_PATHS (comma-separated)
//   - HAISTACK_SYNC_HUB_URL
//   - HAISTACK_SYNC_NODE_ID
//
// StarterYAML returns the bytes written by haistack init.
package config
