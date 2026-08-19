# haistack-proto (`pkg/proto`)

Optional typed protobuf adapter for FHIR resources in the health-ai-stack monorepo.

## What it does

FHIR resources in this stack normally live as **JSON**. That is what gets stored and passed around via `pkg/types`.

This package adds an **optional typed layer** on top. It converts FHIR JSON into **Google FHIR Go protobuf objects** (strongly typed structs under the hood) and back again — without forcing the rest of your code to import Google's libraries.

Think of it as a **translator**:

```
FHIR JSON  ↔  typed proto (in memory)  ↔  ResourceEnvelope
```

Important rules:

- **JSON is still the source of truth** for storage
- **`pkg/types` is still the shared container** (`ResourceEnvelope`)
- **Proto is a companion** — useful for validation and transformation, not a replacement for JSON

It does **not** store data, persist proto blobs, or replace JSON in the database. It only converts between JSON and typed proto values in memory.

## When to use it

Use `pkg/proto` when you want to:

1. **Parse JSON into a typed proto** — Google's parser validates structure as it parses
2. **Convert proto back to canonical JSON** — same normalized format `pkg/types` expects
3. **Build a full envelope with both JSON and proto attached** — hash and metadata from JSON, typed object in `envelope.Proto`

You **do not** need it for basic JSON handling — that is what `pkg/types` is for.

## Usage

**Construct a typed resource and create an envelope:**

```go
import (
	proto "github.com/degoke/health-ai-stack/pkg/proto"
	protor4 "github.com/degoke/health-ai-stack/pkg/proto/r4"
)

patient := protor4.NewPatient("pat-1")
envelope, err := proto.ToEnvelope(patient)
// envelope.JSON is canonical; envelope.Proto is the original patient.
```

For explicit resource-type control or JSON parsing, use a codec:

```go
import "github.com/degoke/health-ai-stack/pkg/proto"

codec := proto.NewGoogleR4Codec()
```

**Parse JSON into proto:**

```go
pb, err := codec.ParseJSONToProto("Patient", patientJSON)
// pb is an `any` — a typed Google R4 ContainedResource inside
```

**Parse JSON into a full envelope in one step:**

```go
envelope, err := codec.ParseJSONToEnvelope("Patient", patientJSON)
// Same as ParseJSONToProto + ProtoToEnvelope
// Or use the package helper: proto.ParseJSONToEnvelope("Patient", patientJSON)
```

**Unwrap the typed proto from an envelope:**

```go
cr, err := proto.ContainedResourceFromEnvelope(envelope)
patient := cr.GetPatient()
```

**Convert proto back to canonical JSON:**

```go
jsonBytes, err := codec.ProtoToJSON("Patient", pb)
// Same normalized JSON rules as pkg/types
```

**Get a full envelope (JSON + metadata + proto attached):**

```go
envelope, err := codec.ProtoToEnvelope("Patient", pb)
// envelope.JSON, envelope.Hash, envelope.ID, ...
// envelope.Proto holds the original typed value
```

**Check what you have:**

```go
if proto.IsProtoResource(envelope.Proto) {
    rt, _ := proto.ResourceTypeOfProto(envelope.Proto) // "Patient"
}
```

When `resourceType` is non-empty, it must match the payload (for example `"Patient"`). Pass `""` to accept whatever type is in the JSON or proto.

## Mental model

| Package | Role |
|---------|------|
| **types** | Works with FHIR as JSON — parse, hash, read id/meta |
| **proto** | Optionally adds typed proto companions on top of that JSON |

```
Patient JSON
    │
    ├─ pkg/types only ──► ResourceEnvelope { JSON, Hash, ... }     Proto = nil
    │
    └─ pkg/proto path ──► ResourceEnvelope { JSON, Hash, ... }     Proto = typed object
```

**One-liner:** `pkg/types` handles FHIR JSON; `pkg/proto` optionally gives you a typed proto version of the same resource, while keeping JSON as the canonical form everywhere else.

## Where it fits

| Layer | Role |
|-------|------|
| **types** | Canonical JSON, envelopes, field helpers |
| **proto** | Optional typed proto companions on envelopes |
| **store** | Persistence contracts using `ResourceEnvelope` |
| **core** | Resource lifecycle (CRUD, history, bundles) |

## MVP limits

- Google FHIR Go R4 only (no R5 yet)
- No proto blob storage in the database
- No profile-aware validation or proto diffing
- `pkg/proto/r4` aliases the pinned Google R4 message types, while JSON remains canonical
- `envelope.Proto` is set on proto paths; JSON-only paths leave it nil
- JSON→proto→JSON may reject or omit fields outside the Google R4 schema; use pkg/types for arbitrary JSON

See [doc.go](./doc.go) for the full API, conversion flows, and future extension points.
