# haistack-conflict (`pkg/conflict`)

FHIR-aware conflict policy and merge library for HAIStack.

## What it does

`pkg/conflict` decides whether a sync conflict can be merged automatically and, when safe, produces a merged `types.ResourceEnvelope` plus a FHIR Patch (JSON Patch) rebase artifact. It keeps the policy layer separate from the sync transport layer: `haistack-sync` detects stale-base failures and enqueues `sync.conflict_processing` jobs; `haistack-conflict` evaluates those conflicts and builds merge or review artifacts.

Main components:

| Component | Role |
|-----------|------|
| `Engine` | High-level entrypoint: `Detect`, `CanAutoMerge`, `Merge` |
| `NewDefaultEngine` / `NewEngine` | Construct engine with built-in or custom policy |
| `Config` | Policy registry, clock, and extension points |
| `Result` | Conflict classification, risk level, changed paths, overlap info, auto-merge flag |
| `MergeResult` | Merged resource, FHIR Patch bytes, resolution metadata, or review metadata |
| `PolicyRegistry` / `RulePack` / `Rule` | Resource/path-specific merge rules |
| `ReviewMetadata` | Human-review reason, path summaries, overlap paths, UI-safe labels |
| diff layer | FHIR-aware JSON diff that emits normalized dotted element paths (`PathChange`) |
| patch builder | JSON Patch operations (`add`, `replace`) that rebase local changes onto the current canonical resource |

`LocalEvent` mirrors `sync.LocalEvent` so the engine can be used without importing `pkg/sync`.

It does **not**:

- Detect stale-base push failures (`pkg/sync` does that)
- Persist conflicts or write audit logs (`pkg/store` and `pkg/sync` do that)
- Apply merged resources locally or resubmit to the hub automatically (the runtime uses `ConflictResolutionHandler` in `pkg/sync`)
- Resolve terminology, profiles, or business rules beyond the configured merge policy

## How it fits in the ecosystem

```
  stale-base push (pkg/sync.PostgresHub)
           |
           v
  ConflictRecord persisted (store.ConflictStore)
           |
           v
  JobTypeConflictProcessing (pkg/jobs)
           |
           v
  pkg/conflict.Engine.Detect / Merge
           |
     +-----+-----+
     v           v
 auto-merge     ReviewMetadata
 MergeResult    (UI / manual workflow)
     |
     v
 ConflictResolutionHandler (pkg/sync) -> replay / resubmit / surface review
```

| Direction | Package | Relationship |
|-----------|---------|--------------|
| Upstream | **sync** | Supplies base/current/local envelopes via job payload; default `ConflictEngine` on `sync.Config` |
| Upstream | **types** | `ResourceEnvelope` JSON for base, current, local, and merged states |
| Downstream | **sync** | `ConflictResolutionHandler` consumes `MergeResult` |
| Sidecar | **store** | Conflict persistence only (not owned by this package) |

## When to use it

- From a `sync.conflict_processing` job handler to evaluate a persisted conflict
- From a runtime or UI that needs to surface changed paths, overlap, and a recommended review reason
- From a manual-resolution flow that wants to preview a merged resource and patch before applying it
- When registering tenant-specific safe-list merge rules via `PolicyRegistry`

## Usage modes

### 1. Default engine: detect and merge

```go
import (
    "github.com/degoke/haistack/pkg/conflict"
)

engine := conflict.NewDefaultEngine()

local := conflict.LocalEvent{
    ResourceType:     "Patient",
    ResourceID:       "p1",
    Operation:        "resource.updated",
    BaseCloudVersion: "base-v1",
    LocalVersion:     "local-v2",
    ResourceAfter:    localEnvelope,
}

result := engine.Detect(local, baseEnvelope, currentEnvelope)

if result.AutoMergeable {
    mergeResult := engine.Merge(local, baseEnvelope, currentEnvelope)
    // mergeResult.Merged.JSON, mergeResult.Patch
} else {
    // result.ReviewReason, result.OverlappingPaths
    // mergeResult.Review.UILabels
}
```

### 2. Classify without merging (`CanAutoMerge`)

```go
result := engine.Detect(local, baseEnvelope, currentEnvelope)
if engine.CanAutoMerge(result) {
    mergeResult := engine.Merge(local, baseEnvelope, currentEnvelope)
}
```

`CanAutoMerge` inspects a prior `Detect` result; `Merge` re-runs detection internally when building artifacts.

### 3. Custom policy registry and rule packs

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

Rule semantics include `RuleSemanticsAutoMerge`, `RuleSemanticsAppendOnly`, and `RuleSemanticsReview`.

### 4. Integration via `pkg/sync` engine config

```go
syncEngine := hasync.NewEngine(hasync.Config{
    NodeID:   "node-a",
    TenantID: "tenant-a",
    // ConflictEngine defaults to conflict.NewDefaultEngine() if nil
    ConflictEngine:            customEngine,
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

### 5. Inspect path-level diffs for tooling

`Detect` populates `Result.LocalChanges` and `Result.RemoteChanges` as `[]PathChange` with `ChangeKind` (`add`, `remove`, `replace`). Use these for custom UIs without calling `Merge`.

### 6. Blocked or unsupported shapes

Operations `resource.created` and `resource.deleted`, missing envelopes, or identity mismatches return `ClassificationUnsupportedMerge` with `RiskLevelBlocked` — callers should route to manual workflows or abort replay.

## Examples

**Non-overlapping concurrent edits (typically review in v1):**

```go
// Local changed Patient.telecom; remote changed Patient.address only.
result := engine.Detect(local, base, current)
// result.Classification may be same_resource_non_overlapping_update
// result.AutoMergeable depends on policy safe-list
```

**Append-only array overlap:**

```go
// Both sides appended to Appointment.note
result := engine.Detect(local, base, current)
// ClassificationAppendOnlyCompatible when policy allows
```

**Clinical hot-spot (forced review):**

```go
// Remote and local both touch Observation.value
result := engine.Detect(local, base, current)
// RiskLevelReview, ClassificationClinicallySensitive or overlapping update
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

## Risk levels

| `RiskLevel` | Typical use |
|-------------|-------------|
| `safe` | Auto-merge candidate under policy |
| `review` | Human review recommended |
| `blocked` | Cannot produce a patch merge |

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

## Configuration / key types

| Type | Fields / notes |
|------|----------------|
| `Engine` | `Detect`, `CanAutoMerge`, `Merge` |
| `Config` | `Registry *PolicyRegistry`, `Clock func() time.Time` |
| `LocalEvent` | Mirrors sync payload: versions, operation, `ResourceAfter`, optional `Patch` |
| `MergeResult` | `Merged`, `Patch []byte`, `Review`, `Resolution`, `AutoMergeable` |
| `Rule` | `ResourceType`, `PathPrefix`, `Semantics`, `Description` |

## Where it fits

| Layer | Role |
|-------|------|
| **sync** | Detects stale-base failures, persists `ConflictRecord`, enqueues jobs, writes audit side effects |
| **conflict** | Semantic diffing, changed-path derivation, mergeability decisions, merge/rebase artifact generation |
| **store** | Conflict and audit persistence |
| **types** | Canonical JSON envelopes used for base, current, and merged states |
| **jobs** | `JobTypeConflictProcessing` delivery |

## Limits

- v1 is conservative: explicit safe-list only for auto-merge
- FHIR Patch is the only resolution artifact; standalone RFC 6902 JSON Patch export is not a separate API
- Delete and create conflicts are classified as unsupported merge shapes
- Tenant-specific custom policies can be registered; per-tenant selection API is minimal (`PolicyRegistry.Select`)
- Rich conflict UI persistence and full resolution audit workflows are deferred; the package produces metadata and `pkg/sync` writes audit actions
- `LocalEvent.ChangedPaths` and `Patch` from sync are not required for `Detect` — diff is recomputed from envelopes

## Related docs

- [docs/architecture.md](../../docs/architecture.md) — sync and conflict flow
- [pkg/sync/README.md](../sync/README.md) — push conflicts, jobs, `ConflictResolutionHandler`
- [pkg/store/README.md](../store/README.md) — `ConflictStore` contract
- [pkg/types/README.md](../types/README.md) — `ResourceEnvelope` and JSON codec
- [pkg/jobs/README.md](../jobs/README.md) — conflict processing job runner
- [doc.go](./doc.go) — package API and file layout
