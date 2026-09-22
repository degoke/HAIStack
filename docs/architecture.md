# Architecture

HAIStack separates **contracts** (`pkg/store`, `pkg/types`) from **implementations** (`pkg/sqlite`, `pkg/postgres`) and **orchestration** (`pkg/core`, `pkg/runtime`, `pkg/http`). Application code should depend on interfaces and envelopes, not on SQL details.

## Layer diagram

```
                    ┌─────────────────────────────────────┐
                    │  Application (HTTP, CLI, custom)    │
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
                                │
                    ┌───────────▼─────────────┐
                    │    pkg/terminology       │
                    │ lookup, validation,      │
                    │ finite ValueSet expansion│
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

Higher layers (`pkg/http`, `pkg/client`, `pkg/export`, `pkg/view`, `pkg/ai`) attach at the sides of this stack—they call into `core`, `search`, or dedicated services rather than bypassing storage contracts.

## Write path

A typical resource write through `core.ResourceService`:

1. **Optional validation** — structural checks via `pkg/validate` or terminology via `pkg/terminology` when enabled  
2. **ID and version** — assign or validate id; bump `meta.versionId` and `meta.lastUpdated`  
3. **Normalize and hash** — `pkg/types` canonical JSON  
4. **Optional hooks** — `pkg/hooks` pre-storage interceptors  
5. **Atomic persist** — one `WriteSession` transaction:
   - current resource row  
   - history row  
   - optional search index update  
   - optional outbox/sync event  
   - optional terminology projection rebuild  
6. **Optional hooks** — post-commit (errors logged, write still succeeds)  

If any step in the transaction fails, the session rolls back.

## Read and search path

- **Instance read** — `ResourceStore.Read` via `core`  
- **History** — `HistoryStore` per resource  
- **Search** — `pkg/search` parses FHIR search parameters, plans against registry metadata, executes against `SearchStore` (Postgres or SQLite backend)  

HTTP exposes these through `pkg/http`; the Go SDK mirrors them in `pkg/client`.

## Sync path

Local nodes append **outbox events** on write. `pkg/sync.Engine`:

- **Push** — send local events to a hub (`PostgresHub` or HTTP hub)  
- **Pull** — fetch canonical events after a cursor  
- **Conflicts** — `pkg/conflict` classifies and merges when bases diverge  

See [pkg/sync/README.md](../pkg/sync/README.md) and [examples/sync-two-nodes](../examples/README.md).

## Transactional vs analytical paths

HAIStack deliberately separates **interactive REST** from **cohort extraction and reporting**:

| Need | Use | Avoid |
|------|-----|-------|
| CRUD, search, history, SDC | `pkg/http` REST | Paginating `_search` to export large cohorts |
| Standard cohort export (NDJSON) | `GET /fhir/$export` via `pkg/export` | Custom REST pagination scripts |
| Structured reporting / AI batch context | `pkg/view` → `pkg/analytics` | Raw bulk export for tabular analytics |
| Incremental analytics refresh | `analytics.IncrementalTarget` + view `Since` | Full table scan every schedule tick |
| Parquet lakehouse layout | `pkg/parquetfhir` via view export or analytics sink | Ad-hoc JSON flattening |

**Decision tree:**

1. Single resource or small result set → REST read/search.  
2. Research/engineering cohort (FHIR as NDJSON) → Bulk Data `$export`.  
3. Dashboards, dbt, or ML features on flat columns → ViewDefinition refresh/export.  
4. AI tool context with policy → `pkg/view` + `pkg/ai` (row-limited, permissioned).  

Edge mode can co-locate OLTP and reporting in one Postgres/SQLite instance. Schedule heavy analytics off peak or use read replicas when available. See [pkg/analytics/EDGE.md](../pkg/analytics/EDGE.md).

## Security layering

| Layer | Package | Responsibility |
|-------|---------|----------------|
| Policy engine | `auth` | Principals, roles, patient compartment, view/AI adapters |
| SMART scopes | `smart` | Scope parsing, launch context, token validation |
| Authorization server | `oauth` | `/authorize`, `/token`, discovery, PKCE (optional embed) |
| HTTP enforcement | `http` | Wires auth adapters into REST handlers |

Design note: [smart-auth-architecture.md](smart-auth-architecture.md).

## Design rule for contributors

**Core packages define interfaces; database packages implement them; HTTP/CLI/runtime glue them together; third-party libraries sit behind adapters.**

When adding a feature, decide which layer owns it before opening a PR. Persistence belongs behind `store.*` interfaces; REST shape belongs in `http`; business invariants belong in `core`.

## Related reading

- [Composition patterns](composition-patterns.md)  
- [Overview](overview.md)  
- Per-package READMEs in [docs/README.md](README.md#package-reference)
