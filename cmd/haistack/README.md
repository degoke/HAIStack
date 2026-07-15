# haistack-cli (`cmd/haistack`)

Developer and operator command-line interface for Health AI Stack.

## What it does

**haistack** is the runnable CLI surface for local development, edge operations, and automation against a Health AI Stack runtime. It loads YAML configuration, applies flag and environment overrides, and delegates to existing libraries (`pkg/runtime`, `pkg/core`, `pkg/validate`, `pkg/search`, `pkg/sync`, `pkg/modules`, `pkg/fhirpath`).

| Concern | What the CLI owns |
|---------|-------------------|
| **Workspace** | `haistack init` writes `haistack.yaml` and `.haistack/` |
| **Configuration** | YAML schema, defaults, env/flag overrides |
| **Runtime** | `serve` builds and starts `pkg/runtime` with managed HTTP |
| **One-shot ops** | Open store, migrate, seed registry, install modules, run a single action |
| **Output** | Human-readable text by default; `--output json` for automation |

It does **not**:

- Replace `pkg/runtime` composition logic (commands call the runtime builder)
- Implement FHIR business rules or persistence (those live in `pkg/*`)
- Call remote sync hubs for health checks (`sync status` is local-store only)
- Provide export, audit inspection, backup/restore, or conflict drill-down (planned later)

## When to use it

- Bootstrapping a local SQLite workspace for development
- Validating and importing JSON FHIR resources from files
- Running FHIR search and FHIRPath evaluation against a configured store
- Starting a local FHIR HTTP server without writing a custom `main`
- Operating device sync (push/pull/status) against a configured hub
- Installing modules and rebuilding search indexes from the shell

Prefer embedding `pkg/runtime` directly when building a custom service binary with non-CLI lifecycle needs.

## Build

```bash
go build -o bin/haistack ./cmd/haistack
# or
make build
```

## Quick start

```bash
# 1. Create workspace config and data directory
haistack init

# 2. Import a resource
haistack import patient.json

# 3. Validate, search, and evaluate FHIRPath
haistack validate patient.json
haistack search Patient name=Smith
haistack fhirpath eval patient.json 'Patient.name.family'

# 4. Start the HTTP server (blocks until interrupted)
haistack serve
```

## Commands

| Command | Description |
|---------|-------------|
| `haistack init` | Write starter `haistack.yaml` and create `.haistack/`. Use `--force` to overwrite. |
| `haistack serve` | Build `pkg/runtime`, start HTTP, print bound address. |
| `haistack validate <file>` | Structural validation via `validate.Engine`. Exits non-zero when invalid. |
| `haistack import <file>` | Upsert one JSON resource (create or update by type/id). |
| `haistack search <ResourceType> [key=value ...]` | FHIR search via `search.Service.SearchBundle`. |
| `haistack fhirpath eval <file> <expression>` | Evaluate FHIRPath against a JSON resource file. |
| `haistack sync push` | One push pass via `sync.Engine`. Requires `sync.hubURL`. |
| `haistack sync pull` | One pull pass via `sync.Engine`. Requires `sync.hubURL`. |
| `haistack sync status` | Local sync view: node ID, hub URL, pull cursor, pending retry-push jobs, unresolved conflicts. |
| `haistack module install <path>` | Install a local module directory via `modules.Manager`. |
| `haistack reindex [ResourceType]` | Synchronous search reindex for one type or all enabled types. |

Run `haistack <command> --help` for flags and examples on each command.

## Configuration

Configuration is read from `haistack.yaml` in the working directory by default (`--config` to override).

### Schema

```yaml
storage:
  driver: sqlite          # sqlite or postgres
  sqlitePath: .haistack/haistack.db
  postgresDSN: ""
  tenantID: ""
runtime:
  httpAddr: 127.0.0.1:8080
  enableSearch: true
  modulePaths:
    - modules/core
sync:
  hubURL: ""
  nodeID: runtime-node
```

### Precedence

1. Built-in defaults
2. YAML file (`haistack.yaml` or `--config`)
3. Environment variables (`HAISTACK_*`)
4. Command-line flags

Relative `sqlitePath` and `modulePaths` entries in a config file are resolved against the config file's directory.

If the default `haistack.yaml` is missing, built-in defaults are used so commands can run before `haistack init`.

### Environment variables

| Variable | Maps to |
|----------|---------|
| `HAISTACK_STORAGE_DRIVER` | `storage.driver` |
| `HAISTACK_SQLITE_PATH` | `storage.sqlitePath` |
| `HAISTACK_POSTGRES_DSN` | `storage.postgresDSN` |
| `HAISTACK_TENANT_ID` | `storage.tenantID` |
| `HAISTACK_HTTP_ADDR` | `runtime.httpAddr` |
| `HAISTACK_ENABLE_SEARCH` | `runtime.enableSearch` |
| `HAISTACK_MODULE_PATHS` | `runtime.modulePaths` (comma-separated) |
| `HAISTACK_SYNC_HUB_URL` | `sync.hubURL` |
| `HAISTACK_SYNC_NODE_ID` | `sync.nodeID` |

### Persistent flags

| Flag | Purpose |
|------|---------|
| `--config` | Path to YAML config file |
| `--output` | `text` (default) or `json` |
| `--storage-driver` | Override storage driver |
| `--sqlite-path` | Override SQLite database path |
| `--postgres-dsn` | Override Postgres DSN |
| `--tenant-id` | Override Postgres tenant ID |
| `--http-addr` | Override HTTP listen address |
| `--enable-search` | Override search enablement (`true`/`false`) |
| `--module-path` | Override module path (repeatable) |
| `--sync-hub-url` | Override sync hub URL |
| `--sync-node-id` | Override sync node ID |

## Storage modes

### SQLite (default)

- Database path defaults to `.haistack/haistack.db`
- Suitable for local development and offline-first nodes
- Search uses embedded/basic indexing (advanced `text.*` keys are skipped on SQLite)

### Postgres

Set in config or via flags/env:

```yaml
storage:
  driver: postgres
  postgresDSN: postgres://user:pass@localhost:5432/haistack?sslmode=disable
  tenantID: tenant-a
```

Postgres mode uses tenant-scoped stores and supports background reindex workers inside `pkg/runtime`. CLI `reindex` still runs synchronously in phase 1.

## Output contract

- **Text (default):** concise human-readable summaries on stdout; errors on stderr
- **JSON (`--output json`):** structured payloads for scripting; errors as `{"error":"..."}`

Server commands (`serve`) print startup metadata; blocking commands return results and exit.

## Package layout

```
cmd/haistack/
  main.go              # entrypoint; calls command.NewRootCommand().Execute()
  doc.go               # package documentation for the binary
  README.md            # this file
  command/             # cobra subcommands and persistent flags
  internal/
    config/            # YAML schema, defaults, load/validate/override
    app/               # runtime session wiring, output helpers, sync status readers
```

- **`command`** defines the CLI tree and maps subcommands to library calls.
- **`internal/config`** is the only configuration package; not imported by `pkg/*`.
- **`internal/app`** opens a `runtime.Runtime` for one-shot commands and shared helpers (`OpenSession`, `BuildRuntime`, `UpsertResource`, sync status inspection).

## Testing

```bash
go test ./cmd/haistack/...
```

Integration tests cover SQLite serve/import/search/reindex and Postgres tenant wiring when `TEST_POSTGRES_DSN` or Docker is available. Use `-short` to skip heavier Postgres/sync cases.

## Related packages

| Package | Role |
|---------|------|
| `pkg/runtime` | Runtime composition and lifecycle (`serve`) |
| `pkg/core` | Resource create/update (`import`) |
| `pkg/validate` | Structural validation (`validate`) |
| `pkg/search` | FHIR search and reindex (`search`, `reindex`) |
| `pkg/sync` | Device push/pull (`sync`) |
| `pkg/modules` | Module installation (`module install`) |
| `pkg/fhirpath` | Expression evaluation (`fhirpath eval`) |

See the [root README](../../README.md) for monorepo architecture and the [runtime README](../../pkg/runtime/README.md) for deployment modes and builder API details.
