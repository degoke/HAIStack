# Health AI Stack

[![CI](https://github.com/degoke/health-ai-stack/actions/workflows/ci.yml/badge.svg)](https://github.com/degoke/health-ai-stack/actions/workflows/ci.yml) ![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**Health AI Stack is a collection of modular Go libraries for building FHIR-native health data infrastructure with safe AI access.**

The libraries can be used independently or composed together to create offline-first local runtimes, edge FHIR servers, cloud repositories, sync engines, analytics layers, and health data tools. It is not a single monolithic FHIR server — it is building blocks for health data systems that run locally, at the edge, on-premise, or in the cloud.

**Module:** `github.com/degoke/health-ai-stack` · **Go:** 1.26+

---

## Why it exists

Healthcare infrastructure is usually always-online and centralized. That breaks down when internet is unreliable, clinics need local-first workflows, field workers capture data offline, hospitals want on-premise control, and AI tools need structured, permissioned access — without a heavy platform just to store, sync, query, or process FHIR data.

FHIR provides a common data model, but practical systems still need infrastructure for storage, search, sync, history, validation, authorization, analytics, and AI-safe access. Health AI Stack makes those pieces modular.

---

## The story

> What if a clinic, mobile health app, or edge health system could keep working when the cloud is unavailable, while still using FHIR as the data foundation?

The stack targets **on-device, offline-first, edge, on-premise, cloud, and AI-assisted** deployments. The goal is reusable libraries — not replacing every FHIR server — that developers compose into their own systems: local SQLite stores, Postgres edge repositories, Git-inspired sync, FHIRPath and search, ViewDefinitions, blob handling, audit/auth primitives, and safe AI tools.

**Use cases:** offline health apps, embedded FHIR stores, edge/cloud backends, device-to-cloud sync, patient intake, clinic scheduling, analytics layers, and AI tools with permissioned FHIR access.

---

## Core ideas

**Modular, not monolithic** — use only what you need (SQLite store alone, FHIRPath alone, full edge stack, etc.).

**Offline-first** — create, update, search, and store locally; push to edge/cloud when connectivity returns.

**Git-inspired sync** — local write = provisional commit; edge/cloud = canonical branch; push/pull/conflict resolution for resource-aware merges.

**Safe AI access** — AI tools call typed, permissioned paths (FHIRPath / views / search) → structured context → audit log, not raw records by default.

**Postgres-first edge** — one Go binary + one Postgres database can hold resources, history, search indexes, sync events, blobs, views, jobs, and audit logs. External services (object storage, OpenSearch, queues) are optional for cloud scale, not required at the edge.

**Technical foundation** — canonical FHIR JSON is the source of truth. Resources flow as `ResourceEnvelope` values (version, hash, timestamps). Business rules live in `pkg/core`; persistence swaps via `pkg/store` contracts and `pkg/sqlite` / `pkg/postgres` adapters.

---

## Example composition

**Local offline runtime:** `core`, `sqlite`, `registry`, `search`, `sync`, `fhirpath`, `http`

**Edge FHIR server:** `core`, `postgres`, `registry`, `search`, `sync`, `conflict`, `binary`, `view`, `auth`, `http`

**AI health data tool:** `ai`, `view`, `fhirpath`, `auth`, `client`

Import paths: `github.com/degoke/health-ai-stack/pkg/…`

---

## Project status

Early-stage, under active development.

| | |
|---|---|
| **Done** | `types`, `proto`, `store`, `sqlite`, `postgres`, `core`, `validate`, `fhirpath`, `registry`, `sync`, `modules`, `view`, `ai`, `auth`, `jobs`, `audit`, `smart`, `subscriptions` — CRUD, history, transaction bundles, atomic writes, structural validation, FHIRPath, FHIR definition catalog, device-to-hub push/pull, manifest-driven module installer, ViewDefinition execution, policy-governed AI tool harness, shared identity and policy library, shared job runtime, shared audit event library, optional SMART on FHIR (scopes, tokens, auth adapters), and change-triggered workflows with webhook/local delivery |
| **Partial** | — |
| **Next (Stage 1)** | `http`, `cli`, `testkit` |

Durable job and audit persistence is available via `pkg/postgres` and `pkg/sqlite`. Shared runtime helpers live in `pkg/jobs` and `pkg/audit`. See [Roadmap](#roadmap) for the full plan.

---

## Architecture

```
                    ┌─────────────────────────────────────┐
                    │  Application (HTTP/gRPC — future)   │
                    └─────────────────┬───────────────────┘
                                      │
                    ┌─────────────────▼───────────────────┐
                    │           pkg/core                  │
                    │  ResourceService, bundles, errors   │
                    └─┬─────────┬─────────┬───────────────┘
                      │         │         │
         ┌────────────┘         │         └────────────┐
         ▼                      ▼                      ▼
  pkg/validate           pkg/search              pkg/sync
  (optional)             (optional)              (optional)
         │                      │                      │
         │                      │                      ▼
         │                      │              pkg/conflict
         │                      │              (optional)
         │                      │                      │
         └──────────────────────┼──────────────────────┘
                                │
                    ┌───────────▼───────────┐
                    │      pkg/registry       │
                    │  FHIR definition catalog│
                    └───────────┬─────────────┘
                                ▼
                    ┌───────────────────────┐
                    │      pkg/store        │
                    │  storage contracts    │
                    └───────────┬───────────┘
                                │
              ┌─────────────────┴─────────────────┐
              ▼                                   ▼
       pkg/sqlite                          pkg/postgres
   (embedded / offline)              (tenant-scoped server)
              │                                   │
              └─────────────────┬─────────────────┘
                                ▼
                    ┌───────────────────────┐
                    │      pkg/types        │
                    │  envelopes, JSON, hash│
                    └───────────┬───────────┘
                                │
                    ┌───────────▼───────────┐
                    │      pkg/proto        │
                    │  optional R4 adapters │
                    └───────────────────────┘
```

**Write path:** validate (optional) → assign id/version → persist resource + history → append outbox event (optional) → update search index (optional) — all in one `WriteSession` transaction.

Package-level detail lives in each `pkg/*/doc.go`.

---

## Packages

| Library | Package | Status | Role |
|---------|---------|--------|------|
| haistack-types | `pkg/types` | Done | FHIR JSON envelopes, canonical normalization, hashing, OperationOutcome |
| haistack-proto | `pkg/proto` | Done | Google FHIR R4 proto adapter; JSON remains canonical |
| haistack-store | `pkg/store` | Done | Storage interfaces — resource, history, search, events, definition catalog, blobs, jobs, audit, `WriteSession` |
| haistack-sqlite | `pkg/sqlite` | Done | Embedded offline DB (pure Go, WAL, migrations, atomic local writes) |
| haistack-postgres | `pkg/postgres` | Done | Tenant-scoped server store; accepted/rejected/conflicted writes; ID registry |
| haistack-registry | `pkg/registry` | Done | Bundled R4 definitions, enablement overlay, compiled snapshot, capability metadata |
| haistack-core | `pkg/core` | Done | FHIR runtime kernel — CRUD, history, transaction bundles, ID policy, errors |
| haistack-sync | `pkg/sync` | Done | Outbox, Engine push/pull, PostgresHub, inbox idempotency, conflict/job hooks |
| haistack-search | `pkg/search` | Done | Registry-driven indexing, FHIR search parser/executor (Postgres), reindex jobs |
| haistack-validate | `pkg/validate` | Done | Built-in structural validation engine; core `Validator` adapter |
| haistack-fhirpath | `pkg/fhirpath` | Done | In-memory FHIRPath engine (Verily-backed); compile, eval, custom functions |
| haistack-conflict | `pkg/conflict` | Done | FHIR-aware conflict detection and merge |
| haistack-modules | `pkg/modules` | Planned | Installable capability modules |
| haistack-view | `pkg/view` | Done | ViewDefinition execution |
| haistack-ai | `pkg/ai` | Done | Policy-governed AI tool harness |
| haistack-auth | `pkg/auth` | Done | Principals, roles, permissions, tenant/device context, policy DSL, view/AI adapters |
| haistack-jobs | `pkg/jobs` | Done | Shared job runtime on `store.JobStore` — handlers, runner, retry/backoff, in-memory store |
| haistack-audit | `pkg/audit` | Done | Shared audit events on `store.AuditStore` — actions, emit helpers, store adapter |
| haistack-smart | `pkg/smart` | Done | Optional SMART on FHIR — scopes, launch context, token/backend-service validation, auth adapters |
| haistack-binary | `pkg/binary` | Done | Blob/file behavior, chunked/resumable transfer, Binary resources, and DocumentReference attachment linking |
| haistack-subscriptions | `pkg/subscriptions` | Done | Change-triggered workflows on `EventStore` with webhook/local delivery, FHIRPath filters, `pkg/jobs` retry, and SQLite/Postgres persistence |
| haistack-analytics | `pkg/analytics` | Done | Postgres-first analytics and reporting engine — ViewDefinition refresh into reporting tables, CSV export, future sink interfaces |
| haistack-http | `pkg/http` | Planned | FHIR REST API adapter |
| haistack-client | `pkg/client` | Planned | Go SDK |
| haistack-runtime | `pkg/runtime` | Planned | Composition and lifecycle glue |
| haistack-cli | `cmd/haistack` | Planned | Developer/operator CLI |
| haistack-testkit | `pkg/testkit` | Planned | Fixtures, fakes, scenario runners |

---

## Getting started

**Requirements:** Go 1.26+ · Docker or `TEST_POSTGRES_DSN` optional (Postgres integration tests)

```bash
git clone https://github.com/degoke/health-ai-stack.git
cd health-ai-stack
go test ./...
```

### Local runtime (SQLite)

```go
import (
    "context"

    "github.com/degoke/health-ai-stack/pkg/core"
    "github.com/degoke/health-ai-stack/pkg/registry"
    "github.com/degoke/health-ai-stack/pkg/sqlite"
    "github.com/degoke/health-ai-stack/pkg/sync"
    "github.com/degoke/health-ai-stack/pkg/types"
)

ctx := context.Background()

db, err := sqlite.Open("/path/to/haistack.db")
if err != nil { /* handle */ }
defer db.Close()

if err := db.Migrate(ctx); err != nil { /* handle */ }

reg := registry.NewManager(registry.Config{
    Definitions: db.DefinitionStore(),
    Installs:    db.RegistryInstallStore(),
})
if err := reg.SeedBundled(ctx); err != nil { /* handle */ }
if err := reg.EnableResource(ctx, "Patient"); err != nil { /* handle */ }
snapshot, err := reg.RebuildSnapshot(ctx)
if err != nil { /* handle */ }

svc, err := core.NewResourceService(core.ResourceServiceConfig{
    Resources: db.ResourceStore(),
    History:   db.HistoryStore(),
    Sessions:  db,
    Outbox:    &sync.EventStoreOutbox{Events: db.OutboxStore()},
})

created, err := svc.Create(ctx, &types.ResourceEnvelope{
    ResourceType: "Patient",
    JSON:         patientJSON,
})
```

### Server runtime (Postgres)

```go
db, err := postgres.Open(ctx, "postgres://user:pass@localhost:5432/haistack?sslmode=disable")
tdb := db.Tenant("tenant-a")
if err := db.EnsureTenant(ctx, "tenant-a"); err != nil { /* handle */ }

reg := registry.NewManager(registry.Config{
    Definitions: db.DefinitionStore(),
    Installs:    tdb.RegistryInstallStore(),
})
_ = reg.SeedBundled(ctx)

envelope, err := tdb.ResourceStore().Read(ctx, "Patient", "pat-1")
```

### Errors → OperationOutcome

```go
if err != nil {
    outcome := core.OperationOutcomeFromError(err)
}
```

Postgres tests: `go test ./pkg/postgres/...` or set `TEST_POSTGRES_DSN` to skip Docker.

---

## Modules

`pkg/modules` provides `haistack-modules`, a manifest-driven installer that turns a local module directory into runtime capabilities by driving the existing registry catalog, `store.ModuleStore`, and `store.RegistryInstallStore`.

A module is a directory containing a `module.json` manifest and optional `definitions/*.json` files:

- `resources` — base FHIR resources to enable.
- `definitionFiles` — FHIR definitions (profiles, search parameters) to ingest.
- `dependencies` — exact module name and minimum semver version.
- `views`, `aiTools`, `permissions`, `syncPolicies`, `subscriptions`, `migrations` — declaration-only arrays stored in metadata for future subsystems.

The public `Manager` API supports `Install`, `Upgrade`, `Uninstall`, `List`, `Inspect`, and `PlanInstall`. v1 is registry-first: views, AI tools, permissions, sync, subscriptions, and migrations are persisted but not executed yet. Example modules live under `modules/`.

## Roadmap

**Design rule:** core packages define interfaces; database packages implement them; HTTP/CLI/runtime packages glue them together; third-party libraries sit behind adapters.

### Build order

| Stage | Packages | Goal |
|-------|----------|------|
| **1 ← current** | types, store, sqlite, core → http, cli, testkit | Local SQLite FHIR runtime; CRUD; history; sync events |
| **2** | search, validate | Search by name/phone/date/status (`search` MVP done; `validate` done) |
| **3** | runtime | Offline create → push → pull → conflict resolution + runtime composition |
| **4** | modules, view | Scheduling module; patient summary and appointment views |
| **5** | auth ✓, ai ✓, jobs ✓, audit ✓ | Safe AI tools with permission checks; shared job/audit libraries |
| **6** | binary, subscriptions ✓, analytics, smart ✓ | Documents, workflows, exports; SMART scope/token/auth adapters |

### Planned package notes

- **search** — MVP done: registry-driven indexer, parser/executor, Postgres backend, reindex jobs via `pkg/jobs`; deferred: chained search, `_include`, OpenSearch, HTTP `_search`
- **sync** — push/pull engine, stale-base detection, conflict job hooks via `pkg/jobs`, and audit via `pkg/audit`; merge policy lives in `pkg/conflict`
- **conflict** — Done in v1: `Engine.Detect`, `CanAutoMerge`, and `Merge`; built-in strict safe-list policy; FHIR Patch rebase artifacts; sync integration via `ConflictResolutionHandler`
- **modules** — manifest-driven bundles of resources, profiles, search params, views, AI tools, permissions
- **view** — SQL-on-FHIR-style ViewDefinitions → structured rows for AI and analytics
- **ai** — typed tool registry; LLMs call tools, not arbitrary FHIR commands; audit via `pkg/audit`
- **auth** — Done in v1: principals/roles/permissions, tenant/device context, policy DSL, view/AI adapters; patient-scope stub; optional decision emit via `AuditingEngine` + `pkg/audit` (auth does not own audit storage); SMART stays in `smart`
- **jobs** — Done in v1: handler/runner, retry/backoff, enqueue helpers, `InMemoryJobStore`; SQLite + Postgres `JobStore` backends
- **audit** — Done in v1: canonical events, emit helpers, `StoreAdapter`; SQLite + Postgres `AuditStore` backends
- **smart** — Done in v1: SMART scope parsing/matching, launch context, token claim validation, backend-service assertions, `AuthAdapter` into `pkg/auth`; no EHR/standalone launch runtime, dynamic registration, or refresh-token lifecycle
- **http** — thin REST over core/search: CRUD, `_history`, `_search`, `metadata`
- **runtime** — wires stores, core, sync, modules, HTTP into local/edge/cloud modes
- **cli** — `serve`, `validate`, `import`, `search`, sync status, `fhirpath eval`

Target layout when complete: `cmd/haistack*`, `pkg/*`, `modules/*`, `examples/*`.

---

## License

License not yet specified in this repository. Add a `LICENSE` file before public distribution.
