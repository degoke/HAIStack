# haistack-core (`pkg/core`)

FHIR resource lifecycle kernel for the health-ai-stack monorepo.

## What it does

`pkg/core` is the **brain for FHIR resource operations**. It knows the rules for creating, reading, updating, and deleting FHIR resources — but it does **not** talk to HTTP or a specific database directly.

Think of it as a layer between your API/server and your storage:

```
HTTP handler (future)  →  pkg/core  →  pkg/store  →  SQLite or Postgres
```

When you save a FHIR resource (like a Patient), core handles the boring but important parts:

1. **Normalizes** the JSON so it is consistent
2. **Optionally validates** it (if you plug in a validator)
3. **Assigns or checks IDs** (uses your ID if valid, otherwise generates a UUID)
4. **Assigns a new version** (`meta.versionId`, `meta.lastUpdated`)
5. **Computes a content hash**
6. **Saves everything atomically**:
   - current resource
   - history entry
   - search index (optional)
   - sync/outbox event (optional)

If anything fails, the whole write is rolled back — no half-written data.

It also supports:

- **Read** — get the current version of a resource
- **History** — get all past versions of one resource
- **Transaction bundles** — create/update/delete multiple resources in one atomic operation
- **Errors → OperationOutcome** — turn Go errors into FHIR error responses

It does **not** store data itself, parse search queries, or serve HTTP. It orchestrates lifecycle rules on top of `pkg/store`.

## What it does not do (MVP)

- HTTP routes or REST server
- Search query parsing (`GET /Patient?name=...`)
- CapabilityStatement generation
- System-wide `_history` (per-resource history only)

## When to use it

- Building a **FHIR server** or API — handlers call `ResourceService` instead of writing DB logic themselves
- Running on **device (SQLite)** or **server (Postgres)** — same core code, different storage backend
- Needing **consistent versioning, history, and rollback** without reimplementing it in every endpoint

## Usage

Wire up storage, create a `ResourceService`, and call methods:

```go
import (
    "context"

    "github.com/degoke/health-ai-stack/pkg/core"
    hasync "github.com/degoke/health-ai-stack/pkg/sync"
    "github.com/degoke/health-ai-stack/pkg/types"
)

// 1. Wire up storage (example: local SQLite)
svc, err := core.NewResourceService(core.ResourceServiceConfig{
    Resources: db.ResourceStore(), // for reads
    History:   db.HistoryStore(), // for history reads
    Sessions:  db,                 // for atomic writes (sqlite.DB or postgres.TenantDB)
    Outbox:    &hasync.EventStoreOutbox{}, // optional: emit change events
    // Validator: myValidator,       // optional
    // Indexer:   myIndexer,         // optional
})
if err != nil {
    // handle config error
}

ctx := context.Background()

// 2. Create a Patient
created, err := svc.Create(ctx, &types.ResourceEnvelope{
    ResourceType: "Patient",
    JSON:         []byte(`{"resourceType":"Patient","name":[{"family":"Doe"}]}`),
})
// created.ID and created.VersionID are set by core

// 3. Read it back
patient, err := svc.Read(ctx, "Patient", created.ID)

// 4. Update it
updated, err := svc.Update(ctx, &types.ResourceEnvelope{
    ResourceType: "Patient",
    ID:           created.ID,
    JSON:         updatedJSON,
})

// 5. Delete it
err = svc.Delete(ctx, "Patient", created.ID)

// 6. Get version history
versions, err := svc.History(ctx, "Patient", created.ID)

// 7. Handle errors for an API response
if err != nil {
    outcome := core.OperationOutcomeFromError(err)
    // return outcome as JSON to the client
}
```

**Transaction bundle** (multiple writes in one atomic session):

```go
resp, err := svc.ProcessTransactionBundle(ctx, &types.ResourceEnvelope{
    ResourceType: "Bundle",
    JSON:         transactionBundleJSON,
})
```

## Required vs optional pieces

| Piece | Required? | Purpose |
|-------|-----------|---------|
| `Resources` | Yes | Read current resources |
| `History` | Yes | Read version history |
| `Sessions` | Yes | Atomic writes (`sqlite.DB` or `postgres.TenantDB`) |
| `IDPolicy` | No | Defaults to FHIR id syntax + UUID generation |
| `Codec` | No | Defaults to `types.NewJSONCodec()` |
| `Validator` | No | Check resource before save |
| `Indexer` | No | Update search tables |
| `Outbox` | No | Emit change events for sync |

## Mental model

**`pkg/core` is “how FHIR resources live and change.”** You plug in storage and optional validation/search/sync; it runs the lifecycle rules for you.

## Where it fits

| Layer | Role |
|-------|------|
| **types** | Canonical JSON and `ResourceEnvelope` |
| **store** | Persistence contracts and write sessions |
| **core** | CRUD, history, versioning, transaction bundles |
| **validate** | Optional pre-write validation contract |
| **search** | Optional index extraction contract |
| **sync** | Optional outbox/event emission contract |
| **sqlite / postgres** | Concrete storage backends |

## Error handling

Core returns typed errors (`invalid`, `conflict`, `not-found`, `not-supported`, `exception`). Map them to FHIR responses with:

```go
outcome := core.OperationOutcomeFromError(err)
```

Helpers `core.IsNotFound(err)` and `core.IsConflict(err)` are available for branching in handlers.

See [doc.go](./doc.go) for the full API, write pipeline, bundle rules, and integration details.
