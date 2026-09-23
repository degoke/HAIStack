# haistack-types (`pkg/types`)

Generic FHIR JSON layer for the haistack monorepo.

## What it does

FHIR resources usually arrive as JSON (`Patient`, `Observation`, and so on). This package lets you work with **any** FHIR resource as JSON **without** importing R4 or R5 generated structs.

It:

1. **Wraps JSON in a standard container** (`ResourceEnvelope`) with resource type, id, version, last-updated time, normalized JSON, and a content hash.
2. **Normalizes JSON** so the same resource always produces the same bytes and hash, even when formatting or key order differs.
3. **Provides small helpers** to read and write common fields (`id`, `meta`) and find references inside nested JSON.
4. **Defines lightweight types** such as `OperationOutcome` for errors, without full FHIR codegen.

It does **not** store data, run searches, validate profiles, or talk to a database. It only handles FHIR JSON shape and metadata.

## How it fits in the ecosystem

`pkg/types` is the JSON source of truth for runtime abstractions. Every layer that moves FHIR data through haistack agrees on the same envelope and canonical bytes:

```
FHIR JSON (wire/storage)
        │
        ▼
  JSONCodec.ParseJSON / NormalizeJSON
        │
        ▼
  ResourceEnvelope { JSON, Hash, ResourceType, ID, VersionID, LastUpdated, Proto? }
        │
        ├──► pkg/store     — persistence contracts (Create/Read/Update/History)
        ├──► pkg/core      — CRUD, bundles, history, event hashes
        ├──► pkg/sync      — push/pull conflict detection via version + content hash
        ├──► pkg/search    — indexing reads envelopes from the store
        ├──► pkg/proto     — optional Proto companion on the same envelope
        └──► pkg/validate  — checks Hash matches re-computed canonical JSON
```

Canonical JSON and `Hash` are always derived from normalized JSON, never from proto fields. When `pkg/proto` sets `envelope.Proto`, it still routes through `JSONCodec.ParseJSON` so metadata stays aligned with JSON-only ingestion.

## When to use it

- Before saving a resource — normalize and hash it for storage or change detection
- When you need type, id, or version without unmarshaling into a large struct
- When walking references in a resource (for example `Patient/123` links)
- As the shared container passed between packages (`core`, `store`, `proto`)
- When building FHIR `OperationOutcome` error bodies without generated models

## Usage modes

### Standalone codec (parse and emit JSON)

Use `ResourceCodec` when you only need canonical bytes and metadata, not persistence:

```go
import "github.com/degoke/haistack/pkg/types"

codec := types.NewJSONCodec()
envelope, err := codec.ParseJSON("", patientJSON)
// envelope.ResourceType, envelope.ID, envelope.JSON, envelope.Hash

out, err := codec.ToJSON(envelope) // same normalized bytes as envelope.JSON
```

`ParseJSON` accepts an optional expected `resourceType`; when non-empty it must match the payload. When empty, the type is read from JSON.

You can normalize without building a full envelope:

```go
canonical, err := types.NormalizeJSON(rawJSON)
rt, err := types.GetResourceType(canonical)
```

### Envelope through core and store

`store.ResourceStore` and `pkg/core` read and write `*types.ResourceEnvelope`. Typical ingest:

```go
envelope, err := types.NewJSONCodec().ParseJSON("Patient", body)
// core.Service.Create(ctx, envelope) → store persists JSON + Hash + meta fields
```

After load, callers can use envelope helpers instead of full struct decode:

```go
active, ok := envelope.BoolField("active")
family, ok := envelope.StringField("name", "0", "family")
count, ok := envelope.Int64Field("multipleBirthInteger")

var dst MyPatientShape
err := envelope.DecodeInto(&dst)
```

`Field`, `StringField`, `BoolField`, and `Int64Field` walk shallow JSON paths for hot reads; `DecodeInto` unmarshals the full document when you need a typed view.

### Hashing for sync and change detection

`HashResource` returns SHA-256 hex of normalized JSON. `ResourceEnvelope.Hash` uses the same algorithm during `ParseJSON`:

```go
hash, err := types.HashResource(patientJSON)

updated, _ := types.SetMeta(patientJSON, types.Meta{VersionID: "2", LastUpdated: time.Now()})
newHash, _ := types.HashResource(updated)
```

Semantically equivalent payloads (different whitespace or key order) normalize to identical hashes. Any semantic change produces a different hash. `pkg/sync` compares version ids and content when applying remote events; `pkg/core` stamps `Hash` on write-path events so consumers can detect no-op replays.

### OperationOutcome and validation carriers

Hand-written types marshal directly with `encoding/json`:

```go
outcome := types.OperationOutcome{
    ResourceType: "OperationOutcome",
    Issue: []types.OperationIssue{{
        Severity:    "error",
        Code:        "invalid",
        Diagnostics: "Patient.name is required",
        Expression:  []string{"Patient.name"},
    }},
}
body, _ := json.Marshal(outcome)
```

`pkg/core` maps service errors to `OperationOutcome` via `OperationOutcomeFromError`. Custom client-side validation can implement `ClientValidationOutcomeError` so HTTP layers return structured outcomes:

```go
type myValidationErr struct{ outcome types.OperationOutcome }
func (e myValidationErr) Error() string { return e.outcome.Issue[0].Diagnostics }
func (e myValidationErr) OperationOutcome() types.OperationOutcome { return e.outcome }
```

`Code` is a plain string in MVP (not a `CodeableConcept`).

## Usage (field helpers and references)

**Read or update `id` / `meta` without a typed struct:**

```go
id, _ := types.GetID(patientJSON)

updated, _ := types.SetID(patientJSON, "new-id")
updated, _ := types.SetMeta(updated, types.Meta{
    VersionID:   "2",
    LastUpdated: time.Now(),
})
```

**Find all references in a resource:**

```go
refs, _ := types.GetReferences(observationJSON)
for _, ref := range refs {
    // ref.Raw is always set; ref.ResourceType/ID for "Patient/123" style refs
}
```

Reference parsing rules:

| Input shape | `ResourceType` / `ID` | `Raw` |
|-------------|------------------------|-------|
| `Patient/123` | populated | original string |
| `only-an-id` (no slash, not URL/URN/#) | empty | original string |
| `https://…`, `urn:…`, `#fragment` | empty | original string |

## Mental model

Think of it as **FHIR JSON utilities plus a standard envelope** — not a full FHIR server or validator, but the common foundation so every other package can handle resources the same way.

## Where it fits

| Layer | Role |
|-------|------|
| **types** | Canonical JSON, envelopes, field helpers |
| **proto** | Optional typed proto companions on envelopes |
| **store** | Persistence contracts using `ResourceEnvelope` |
| **core** | Resource lifecycle (CRUD, history, bundles) |
| **sync** | Device/hub replication using envelope hash and version |
| **validate** | Verifies envelope hash matches canonical JSON |

## Limits

- Version-agnostic FHIR JSON only (no generated R4/R5 bindings here)
- No bundle helpers, extension helpers, profile validation, or identifier extraction
- `ResourceEnvelope.Proto` is set by `pkg/proto` on proto paths; JSON-only paths leave it nil
- `JSONCodec.ToJSON` does not serialize from `Proto`; use `pkg/proto` for proto-to-JSON conversion

## Related docs

- [pkg/core/README.md](../core/README.md) — CRUD and envelopes on the write path
- [pkg/store/README.md](../store/README.md) — persisted envelope shape
- [pkg/proto/README.md](../proto/README.md) — optional typed proto companion
- [doc.go](./doc.go) — canonical JSON rules and reference parsing
