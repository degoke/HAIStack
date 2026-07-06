# haistack-conflict (`pkg/conflict`)

FHIR-aware conflict policy and merge library for Health AI Stack.

## What it does

`pkg/conflict` decides whether a sync conflict can be merged automatically and, when safe, produces a merged `ResourceEnvelope` plus a FHIR Patch (JSON Patch) rebase artifact. It keeps the policy layer separate from the sync transport layer: `haistack-sync` detects stale-base failures and enqueues `sync.conflict_processing` jobs; `haistack-conflict` evaluates those conflicts and builds merge or review artifacts.

Main components:

| Component | Role |
|-----------|------|
| `Engine` | High-level entrypoint: `Detect`, `CanAutoMerge`, `Merge` |
| `Config` | Policy registry, clock, and future extension points |
| `Result` | Conflict classification, risk level, changed paths, overlap info, auto-merge flag |
| `MergeResult` | Merged resource, FHIR Patch, resolution metadata, or review metadata |
| `PolicyRegistry` / `RulePack` / `Rule` | Resource/path-specific merge rules |
| `ReviewMetadata` | Human-review reason, path summaries, overlap paths, UI-safe labels |
| diff layer | FHIR-aware JSON diff that emits normalized dotted element paths |
| patch builder | JSON Patch operations (`add`, `replace`) that rebase local changes onto the current canonical resource |

It does **not**:

- Detect stale-base push failures (`pkg/sync` does that)
- Persist conflicts or write audit logs (`pkg/store` and `pkg/sync` do that)
- Apply merged resources locally or resubmit to the hub automatically (the runtime uses `ConflictResolutionHandler` in `pkg/sync`)
- Resolve terminology, profiles, or business rules beyond the configured merge policy

## When to use it

- From a `sync.conflict_processing` job handler to evaluate a persisted conflict
- From a runtime or UI that needs to surface changed paths, overlap, and a recommended review reason
- From a manual-resolution flow that wants to preview a merged resource and patch before applying it

## Usage

### Basic detect / classify / merge

```go
import (
    "context"

    "github.com/degoke/health-ai-stack/pkg/conflict"
    "github.com/degoke/health-ai-stack/pkg/types"
)

engine := conflict.NewDefaultEngine()

local := conflict.LocalEvent{
    ResourceType:     "Patient",
    ResourceID:       "p1",
    Operation:        "resource.updated",
    BaseCloudVersion: "base-v1",
    LocalVersion:     "local-v2",
    ResourceAfter:   localEnvelope,
}

result := engine.Detect(local, baseEnvelope, currentEnvelope)

if result.AutoMergeable {
    mergeResult := engine.Merge(local, baseEnvelope, currentEnvelope)
    // mergeResult.Merged.JSON
    // mergeResult.Patch (FHIR Patch / JSON Patch bytes)
} else {
    // result.ReviewReason
    // result.OverlappingPaths
    // mergeResult.Review.UILabels
}
```

### Custom policy registry

```go
registry := conflict.DefaultPolicyRegistry()
registry.Register(conflict.RulePack{
    Name: "tenant-a",
    Rules: []conflict.Rule{
        {
            ResourceType: "Patient",
            PathPrefix:   "Patient.contact",
            Semantics:    conflict.RuleSemanticsAppendOnly,
            Description:  "Allow contact append merges",
        },
    },
})
registry.Select("tenant-a")

engine := conflict.NewEngine(conflict.Config{Registry: registry})
```

### From `pkg/sync`

```go
syncEngine := hasync.NewEngine(hasync.Config{
    NodeID:   "node-a",
    TenantID: "tenant-a",
    // ConflictEngine defaults to conflict.NewDefaultEngine() if nil
    ConflictResolutionHandler: myHandler,
})
```

The handler receives the `conflict.MergeResult` for both auto-merge and review outcomes:

```go
type MyHandler struct{}

func (h *MyHandler) OnConflictResolution(
    ctx context.Context,
    payload hasync.ConflictJobPayload,
    result conflict.MergeResult,
) error {
    if result.AutoMergeable {
        // replay or resubmit result.Merged
    } else {
        // surface result.Review for UI
    }
    return nil
}
```

## Classification buckets

| Classification | Meaning |
|----------------|---------|
| `no_conflict` | Local and remote changes are both empty (semantic) |
| `stale_base_only` | Only one side changed relative to the stale base |
| `same_resource_non_overlapping_update` | Both sides changed, but on distinct element paths |
| `same_resource_overlapping_update` | Both sides changed the same path or a nested path |
| `append_only_compatible` | Overlap is on an append-only array and both sides are pure appends |
| `clinically_sensitive_conflict` | Risk tier `review` due to a clinical hot-spot path |
| `unsupported_merge_shape` | Delete/create conflicts, missing payloads, or shapes that cannot be expressed as FHIR Patch |

## Default v1 policy (strict safe-list)

Auto-merge is allowed only for explicit safe-list paths:

| Resource | Path | Semantics |
|----------|------|-----------|
| `Patient` | `Patient.telecom` | `auto_merge` |
| `Patient` | `Patient.address` | `auto_merge` |
| `Appointment` | `Appointment.note` | `append_only` |
| `Encounter` | `Encounter.statusHistory` | `append_only` |

Everything else defaults to human review, including these clinical hot-spots:

| Resource | Path |
|----------|------|
| `MedicationRequest` | `MedicationRequest.dosageInstruction` |
| `AllergyIntolerance` | `AllergyIntolerance.clinicalStatus` |
| `Observation` | `Observation.value` |
| `Consent` | `Consent.provision` |
| `Appointment` | `Appointment.start` |
| `Patient` | `Patient.birthDate` |

## Where it fits

```
┌─────────────┐     ┌─────────────┐     ┌─────────────────┐
│  pkg/sync   │────▶│ pkg/conflict│────▶│  runtime / UI   │
│ detects and │     │ classifies  │     │ replay, review, │
│  enqueues   │     │ builds patch│     │ or resubmit     │
└─────────────┘     └─────────────┘     └─────────────────┘
        │                  │
        ▼                  ▼
   pkg/store        pkg/types.ResourceEnvelope
   ConflictRecord   JSON payloads
```

| Layer | Role |
|-------|------|
| **sync** | Detects stale-base failures, persists `ConflictRecord`, enqueues jobs, writes audit side effects |
| **conflict** | Semantic diffing, changed-path derivation, mergeability decisions, merge/rebase artifact generation |
| **store** | Conflict and audit persistence |
| **types** | Canonical JSON envelopes used for base, current, and merged states |

## Current limits

- v1 is conservative: explicit safe-list only for auto-merge
- FHIR Patch is the only resolution artifact; JSON Patch is not implemented separately
- Delete and create conflicts are classified as unsupported merge shapes
- Tenant-specific custom policies can be registered, but the API surface for per-tenant selection is minimal
- Rich conflict UI persistence and full resolution audit workflows are deferred; the package produces the metadata and the audit actions are written by `pkg/sync`

See [doc.go](./doc.go) for the package API and file layout.
