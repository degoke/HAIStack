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

## When to use it

- Before saving a resource — normalize and hash it for storage or change detection
- When you need type, id, or version without unmarshaling into a large struct
- When walking references in a resource (for example `Patient/123` links)
- As the shared container passed between packages (`core`, `store`, `proto`)

## Usage

**Parse JSON into an envelope:**

```go
import "github.com/degoke/haistack/pkg/types"

codec := types.NewJSONCodec()
envelope, err := codec.ParseJSON("Patient", patientJSON)
// envelope.ResourceType, envelope.ID, envelope.JSON, envelope.Hash
```

**Get a stable hash for deduping or change detection:**

```go
hash, err := types.HashResource(patientJSON)
```

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

**Serialize back to JSON:**

```go
out, err := codec.ToJSON(envelope) // returns normalized JSON bytes
```

## Mental model

Think of it as **FHIR JSON utilities plus a standard envelope** — not a full FHIR server or validator, but the common foundation so every other package can handle resources the same way.

## Where it fits

| Layer | Role |
|-------|------|
| **types** | Canonical JSON, envelopes, field helpers |
| **proto** | Optional typed proto companions on envelopes |
| **store** | Persistence contracts using `ResourceEnvelope` |
| **core** | Resource lifecycle (CRUD, history, bundles) |

## MVP limits

- Version-agnostic FHIR JSON only (no generated R4/R5 bindings here)
- No bundle helpers, extension helpers, profile validation, or identifier extraction
- `ResourceEnvelope.Proto` is set by `pkg/proto` on proto paths; JSON-only paths leave it nil

See [doc.go](./doc.go) for the full API, canonical JSON rules, and reference parsing behavior.
