# Composition patterns

HAIStack libraries compose into common deployment shapes. Each pattern lists **primary packages**, **typical entrypoint**, and **when to choose something else**.

Import paths use the prefix `github.com/degoke/haistack/pkg/…`.

## Local offline runtime (SQLite)

**Goal:** Embedded or mobile node that works without network; optional sync later.

| Packages | Role |
|----------|------|
| `sqlite`, `store`, `types` | Local persistence |
| `registry`, `modules` | Enable Patient, Observation, etc. |
| `core` | CRUD and history |
| `search` | Local `_search` |
| `sync` | Outbox + push/pull when a hub URL exists |
| `fhirpath` | Expressions in app logic or SDC |

**Entrypoints:**

- Manual wiring — [examples/manual-sqlite](../examples/manual-sqlite/main.go)  
- Managed runtime + HTTP — [examples/runtime-http](../examples/runtime-http/main.go) or `haistack serve`  

**Choose Postgres instead when** you need multi-tenant server semantics, accepted/rejected writes, or heavier concurrent search on one host.

---

## Edge FHIR server (Postgres all-in-one)

**Goal:** Single-tenant or per-tenant edge server: REST, search, sync hub, blobs, jobs, audit in one database.

| Packages | Role |
|----------|------|
| `postgres`, `store` | Tenant-scoped stores |
| `registry`, `core`, `search` | FHIR server kernel |
| `sync` | Hub for device push/pull |
| `conflict` | Merge policy on stale bases |
| `binary` | Documents and attachments |
| `auth`, `smart`, `oauth` | Identity (embed OAuth or external IdP) |
| `http`, `runtime` | REST surface and lifecycle |
| `export`, `bulkimport` | Bulk Data (optional) |
| `subscriptions`, `jobs` | Workflows and background work |

**Entrypoints:**

- [examples/edge-postgres](../examples/edge-postgres/main.go)  
- `runtime.New().WithPostgresAllInOne(...).WithHTTP(...).Build`  

**Choose cloud shape when** you must plug in external blob store, OpenSearch, or warehouse adapters (`runtime.WithExternal*`).

---

## Cloud repository (Postgres + external adapters)

**Goal:** Same FHIR API as edge, but scale-out seams for object storage and search.

| Packages | Same as edge, plus |
|----------|-------------------|
| `runtime` cloud mode | `WithExternalBlobStore`, `WithExternalSearch`, `WithExternalWarehouse` |

**Entrypoint:** [examples/cloud-postgres](../examples/cloud-postgres/main.go)

---

## Device ↔ hub sync

**Goal:** Two or more SQLite nodes exchanging events through a hub.

| Packages | Role |
|----------|------|
| `sqlite`, `sync`, `core` | Local node |
| `postgres` or in-process hub | Canonical store (see sync-two-nodes example) |
| `conflict` | Resolution handlers |
| `client` | HTTP push/pull to remote hub |

**Entrypoint:** [examples/sync-two-nodes](../examples/sync-two-nodes/main.go)

---

## Questionnaire / SDC workflow

**Goal:** Populate, validate, assemble, and extract QuestionnaireResponse data.

| Packages | Role |
|----------|------|
| `sdc` | SDC operations and adapter contracts |
| `cql`, `fhirpath` | Calculated fields and expressions |
| `structuremap`, `conceptmap` | Extraction maps and translate() |
| `core`, `registry`, `store` | Canonical resources |
| `http` or `runtime` | `$populate`, `$validate`, `$extract` routes |

**Entrypoints:**

- `runtime` with `modules/sdc` — see [pkg/sdc/README.md](../pkg/sdc/README.md)  
- Conformance IGs under `modules/sdc/ig`  

---

## AI-assisted clinical or admin tools

**Goal:** LLM or agent calls typed tools with policy, not raw SQL or unrestricted REST.

| Packages | Role |
|----------|------|
| `ai` | Tool registry and executor |
| `auth` | Allow-lists and patient compartment |
| `view` | Structured row context |
| `fhirpath`, `search` | Backing reads inside tools |
| `audit` | Tool invocation records |
| `client` | If the FHIR server is remote |

**Entrypoint:** [examples/ai-authz](../examples/ai-authz/main.go)

**Prefer bulk export** only for offline batch pipelines—not for interactive agent turns.

---

## Analytics and reporting

**Goal:** Flat tables, CSV, or Parquet from FHIR via ViewDefinitions.

| Packages | Role |
|----------|------|
| `view` | Execute ViewDefinitions |
| `analytics` | Refresh reporting tables, incremental cursors |
| `parquetfhir` | Spec-aligned nested Parquet (`_parquetLayout=fhir`) |
| `postgres` or `sqlite` | Co-located reporting schema on edge |

**Avoid** using `_search` pagination as a poor person's ETL. Use `$export` for resource-shaped cohorts or `analytics` for columnar reporting.

---

## Bulk Data migration

**Goal:** NDJSON export/import between environments.

| Direction | Packages |
|-----------|----------|
| Export | `export`, `http`, `jobs`, `client.BulkExport` |
| Import | `bulkimport`, `http`, `jobs` |

Operational verification: [bulk-data-verification.md](bulk-data-verification.md).

---

## Conformance and IG development

**Goal:** Author profiles in FSH, build IG JSON, validate examples in CI.

| Path | Role |
|------|------|
| `conformance/` | FSH, SUSHI, example fixtures |
| `pkg/conformance` | Validator config for CI |
| `pkg/validate`, `pkg/registry` | Runtime validation and catalog |

See [conformance/README.md](../conformance/README.md).

---

## Testing and CI helpers

**Goal:** Reusable fakes and scenario catalogs.

| Package | Use |
|---------|-----|
| `testkit` | Fixtures, store/sync/view fakes, authz scenarios |
| `testkit/infernotest` | SMART Inferno-oriented checks |

---

## Quick import cheat sheet

```text
Local SQLite FHIR:     types, sqlite, registry, core, search, sync?, fhirpath?, http?, runtime?
Edge server:           postgres, registry, core, search, sync, conflict, auth, http, runtime
SDC forms:             sdc, cql, fhirpath, structuremap, core, runtime
AI gateway:            ai, auth, view, audit (+ client if remote)
Analytics:             view, analytics, parquetfhir?, postgres
Bulk migrate:          export, bulkimport, client, jobs
CLI / dev workspace:     cmd/haistack → runtime
```

For package-level API detail, follow the links in [docs/README.md](README.md#package-reference).
