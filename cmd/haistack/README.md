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
- Provide backup/restore or conflict drill-down (planned later)

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

`haistack --version` reports `dev` for local builds. Stamp a version at link time with `-ldflags "-X github.com/degoke/health-ai-stack/cmd/haistack/command.Version=vX.Y.Z"` if you need a specific string.

## Quick start

```bash
# 1. Create workspace config and data directory
haistack init

# 2. Optional: install the local module that enables the resource types you use
haistack module install modules/core

# 3. Import a resource
haistack import patient.json

# 4. Validate, search, and evaluate FHIRPath
haistack validate patient.json
haistack search Patient name=Smith
haistack fhirpath eval patient.json 'Patient.name.family'

# 5. Start the HTTP server (blocks until interrupted)
haistack serve
```

## Commands

| Command | Description |
|---------|-------------|
| `haistack init` | Write starter `haistack.yaml` and create `.haistack/`. Use `--force` to overwrite. |
| `haistack serve` | Build `pkg/runtime`, start HTTP, print bound address. |
| `haistack validate <file>` | Structural validation via `validate.Engine`. Exits non-zero when invalid. |
| `haistack import <file>` | Import one JSON resource; use `--create-only` or `--update-only` to control conflicts. |
| `haistack read <ResourceType/id>` | Read one stored resource. |
| `haistack delete <ResourceType/id> --force` | Delete one stored resource. |
| `haistack export <ResourceType[/id]>` | Export one resource or a resource collection as JSON. |
| `haistack search <ResourceType> [key=value ...]` | FHIR search via `search.Service.SearchBundle`. |
| `haistack fhirpath eval <file> <expression>` | Evaluate FHIRPath against a JSON resource file. |
| `haistack sync push` | One push pass via `sync.Engine`. Requires `sync.hubURL`. |
| `haistack sync pull` | One pull pass via `sync.Engine`. Requires `sync.hubURL`. |
| `haistack sync status` | Local sync view: node ID, hub URL, pull cursor, pending retry-push jobs, unresolved conflicts. |
| `haistack module install <path>` | Install or upgrade a local module directory via `modules.Manager`. |
| `haistack module upgrade <path>` | Explicitly upgrade an installed module. |
| `haistack module plan <path>` | Preview module installation or upgrade changes. |
| `haistack module list` / `inspect` / `uninstall` | Inspect and manage installed modules. |
| `haistack config show` / `validate` | Inspect or validate resolved configuration. |
| `haistack audit list` | Inspect persisted audit records. |
| `haistack reindex [ResourceType]` | Synchronous search reindex for one type or all enabled types. |

Run `haistack <command> --help` for flags and examples on each command.

## Configuration

Configuration is read from `haistack.yaml` in the working directory by default (`--config` to override).

### Schema

```yaml
storage:
  driver: sqlite          # sqlite or postgres
  sqlitePath: .haistack/haistack.db
  sqliteTenantID: local
  sqliteTerminologyScope: default
  postgresDSN: ""
  tenantID: ""
runtime:
  httpAddr: 127.0.0.1:8080
  enableSearch: true
  modulePaths: []
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

If the default `haistack.yaml` is missing, built-in defaults are used so commands can run before `haistack init`. Module paths are opt-in; use `--module-path` or configure `runtime.modulePaths` to load local modules.

### Environment variables

| Variable | Maps to |
|----------|---------|
| `HAISTACK_STORAGE_DRIVER` | `storage.driver` |
| `HAISTACK_SQLITE_PATH` | `storage.sqlitePath` |
| `HAISTACK_SQLITE_TENANT_ID` | `storage.sqliteTenantID` |
| `HAISTACK_SQLITE_TERMINOLOGY_SCOPE` | `storage.sqliteTerminologyScope` |
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
| `--sqlite-tenant-id` | Override SQLite sync tenant namespace |
| `--sqlite-terminology-scope` | Override SQLite terminology namespace |
| `--postgres-dsn` | Override Postgres DSN |
| `--tenant-id` | Override Postgres tenant ID |
| `--http-addr` | Override HTTP listen address |
| `--enable-search` | Override search enablement (`true`/`false`) |
| `--no-search` | Disable search for this command |
| `--no-modules` | Disable module loading for this command |
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

Server commands (`serve`) print startup metadata and expose `/healthz` and `/readyz`; blocking commands return results and exit. `--output json` is supported by all command results, including `init`, `serve`, and sync push/pull.

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
