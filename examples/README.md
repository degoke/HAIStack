# Examples

Runnable example applications for different ways to use Health AI Stack.

Each example is a small standalone `main` package that can be run from the
repository root with `go run`.

## Available examples

### `manual-sqlite`

Direct package composition with:

- `pkg/sqlite`
- `pkg/registry`
- `pkg/fhirpath`
- `pkg/search`
- `pkg/core`

Creates a local SQLite-backed stack, writes a couple of `Patient` resources,
runs a FHIR search, and prints the results.

```bash
go run ./examples/manual-sqlite
```

### `runtime-http`

Runtime-managed composition with:

- `pkg/runtime`
- `pkg/http`

Builds a runtime, starts a managed HTTP server, creates a demo `Patient`,
queries the FHIR REST endpoints over HTTP, prints the responses, and shuts down.

```bash
go run ./examples/runtime-http
```

### `edge-postgres`

Edge/server deployment with:

- `pkg/runtime`
- `pkg/postgres`
- managed HTTP
- embedded search on Postgres

Uses `TEST_POSTGRES_DSN` when set, otherwise starts a disposable Postgres
container through testcontainers.

```bash
go run ./examples/edge-postgres
```

### `cloud-postgres`

Cloud deployment shape with:

- `pkg/runtime`
- `pkg/postgres`
- external adapter seams for blob/search/warehouse

This demonstrates the `cloud-postgres-plus-external-services` runtime mode.
Like the edge example, it uses `TEST_POSTGRES_DSN` or a disposable Postgres
container.

```bash
go run ./examples/cloud-postgres
```

### `sync-two-nodes`

Offline-first replication with:

- two local SQLite nodes
- `pkg/sync`
- an in-process example hub

Writes a resource on node A, pushes to the hub, pulls to node B, and verifies
the resource is available on node B.

```bash
go run ./examples/sync-two-nodes
```

### `ai-authz`

Governed AI access with:

- `pkg/auth`
- `pkg/ai`
- `pkg/core`
- `pkg/search`

Builds a local stack, creates demo data, then executes AI read and search tools
through an auth-backed policy adapter.

```bash
go run ./examples/ai-authz
```

## Notes

- The examples use temporary SQLite databases and clean them up automatically.
- The SQLite-only examples do not require Docker or Postgres.
- The Postgres deployment examples require either Docker or `TEST_POSTGRES_DSN`.
- The runtime example loads the local `modules/core` module from this repository.
