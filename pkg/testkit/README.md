# haistack-testkit (`pkg/testkit`)

Shared importable test support for Health AI Stack: FHIR fixtures, in-memory store
fakes, sync and conflict scenario runners, OperationOutcome golden helpers,
FHIRPath assertions, and AI executor harnesses.

**This package is for tests only.** Production code must not import `pkg/testkit` or
any subpackage.

## What it does

`pkg/testkit` consolidates helpers that were duplicated across package-local `_test.go`
files into importable Go packages (not `_test.go` sources). Downstream tests can share:

| Subpackage | Role |
|------------|------|
| **fixtures** | Stable named presets (`PatientJane`, `AppointmentBooked`, …) as `*types.ResourceEnvelope` |
| **factories** | Option-based builders for Patient, Appointment, Observation |
| **storetest** | In-memory `pkg/store` implementations and a composed `Backend` |
| **synctest** | Fake `sync.Hub`, device nodes, push/pull scenario runners |
| **conflicttest** | Conflict detect/merge scenarios on top of sync fakes |
| **golden** | Canonical `OperationOutcome` JSON comparison (inline goldens) |
| **fhirpathtest** | FHIRPath evaluation and assertion wrappers |
| **aitest** | Reusable `ai.Executor` harness with optional search/views/core |

It does **not**:

- Replace production persistence (`pkg/sqlite`, `pkg/postgres`, future `pkg/store/memory`)
- Ship runtime behavior or production adapters
- Delete existing package-local test helpers (migration is incremental)

## When to use it

- Writing sync tests without copying `memHub` / `memResourceStore` from `pkg/sync/helpers_test.go`
- Writing store contract tests without copying mem stores from `pkg/store/store_test.go`
- Building AI tool tests without copying `pkg/ai/fixtures_test.go`
- Asserting `OperationOutcome` payloads from HTTP, client, core, or validate layers
- Running FHIRPath expressions against shared patient/observation fixtures

## Quick start — offline patient sync

```go
import (
    "context"
    "testing"

    "github.com/degoke/health-ai-stack/pkg/testkit/fixtures"
    "github.com/degoke/health-ai-stack/pkg/testkit/synctest"
)

func TestOfflinePatientSync(t *testing.T) {
    ctx := context.Background()
    scenario := synctest.NewScenario(
        "tenant-a",
        synctest.FixedClock(synctest.At(2026, 7, 6, 12, 0, 0)),
    )

    patient := fixtures.PatientJane(t)
    result, err := synctest.OfflineCreateAndSync(ctx, scenario, patient)
    if err != nil {
        t.Fatal(err)
    }
    if result.PullSummary.Applied != 1 {
        t.Fatalf("pull = %+v", result.PullSummary)
    }
}
```

## fixtures

Named presets return normalized envelopes via `types.JSONCodec`:

```go
patient := fixtures.PatientJane(t)
appt := fixtures.AppointmentForPatient(t, patient.ID)
obs := fixtures.ObservationForPatient(t, patient.ID)
```

`OfflinePatientCreate` is an alias for the standard offline-create patient preset.
Use `EnvelopeFromProtoJSON` when a test needs `envelope.Proto` populated.

## factories

Builders accept functional options and return errors on invalid input:

```go
patient, err := factories.NewPatient(
    factories.WithPatientID("pat-custom"),
    factories.WithFamilyName("Smith"),
    factories.WithTelecom("555-9999"),
)
appt, err := factories.NewAppointment(
    factories.WithPatientReference(patient.ID),
    factories.WithAppointmentStatus("booked"),
)
```

Prefer **fixtures** for stable cross-package scenarios; use **factories** when
parameterizing ids, references, status, timestamps, or arbitrary FHIR metadata
(`WithPatientMeta`, `WithAppointmentMeta`, and `WithObservationMeta`).

## storetest

### Backend bundles

```go
device := storetest.NewDeviceBackend()  // lenient reads — sync device semantics
strict := storetest.NewStrictBackend()  // errors on missing resources
```

`Backend` exposes: `Resources`, `History`, `Events`, `Cursors`, `Inbox`,
`Conflicts`, `Search`, `Audit`, and `Jobs` (`jobs.NewInMemoryJobStore`).

`ListIDs` is sorted and paged deterministically. `WriteSessionProvider` commits
resource, history, search, and event snapshots together; `Rollback` discards them.

### Strict vs lenient `ResourceStore`

| Constructor | `Read` when missing |
|-------------|---------------------|
| `NewResourceStore` | error |
| `NewLenientResourceStore` | `nil, nil` |

Match the constructor to the test: core/store tests use strict; sync device tests
use lenient.

### Seeding

```go
ctx := context.Background()
_ = backend.Resources.Seed(ctx, patient, appt)
```

## synctest

### MemHub

`NewMemHub()` implements `sync.Hub`:

- Idempotent push via processed event IDs (`AckAlreadyProcessed`)
- Optional stale-base conflicts: `hub.SetStaleOnMismatch(true)`
- Canonical event log: `hub.CanonicalEvents()`

### Device and scenario

```go
hub := synctest.NewMemHub()
device := synctest.NewDevice("device-a", "tenant-a", hub, clock)

_ = device.SeedLocalCreate(ctx, patient, clock())
push, _ := device.Push(ctx)

scenario := synctest.NewScenario("tenant-a", clock)
result, _ := synctest.OfflineCreateAndSync(ctx, scenario, patient, appt)

resolved, _ := synctest.ReferenceResolved(ctx, scenario.DeviceB, appt,
    "participant.0.actor", "Patient", "pat-jane")
```

`ScenarioResult` includes `PushSummary`, `PullSummary`, `HubEvents`,
`DeviceBResources`, `Conflicts`, and `AuditRecords`.

## conflicttest

```go
scenario := conflicttest.NewScenario("tenant-a", clock)
edits, _ := conflicttest.DefaultConcurrentPatientEdits()

eval := scenario.Evaluate(
    conflicttest.LocalUpdate("Patient", "p1", edits.Base.VersionID, edits.LocalA.VersionID, edits.LocalA),
    edits.Base, edits.Cloud,
)

result, _ := scenario.RunTwoNodeStaleBaseConflict(ctx, edits)
merged, _ := scenario.RunAutoMergeResolution(ctx, edits)
```

`Result` exposes classification, merge metadata, push summary, conflict records,
resolution push results, canonical events, and whether the path was auto-mergeable
or review-required.

## golden

Inline golden JSON only in v1 (no on-disk `testdata` workflow yet):

```go
outcome := golden.DecodeOutcome(t, responseBody)
golden.AssertOutcomeCode(t, outcome, "not-found")
golden.AssertOutcomeMatchesGolden(t, outcome, `{
    "resourceType": "OperationOutcome",
    "issue": [{"severity":"error","code":"not-found"}]
}`)
```

`AssertOutcomeEqual` compares canonical JSON, ignoring whitespace differences.
`FormatMismatch` produces readable diagnostics on failure.

## fhirpathtest

```go
eng := fhirpathtest.DefaultEngine(t)
patient := fixtures.PatientJane(t)

fhirpathtest.AssertString(t, eng, patient, "Patient.name.family", "Doe")
fhirpathtest.AssertEmpty(t, eng, patient, "Patient.address")
```

Uses `pkg/fhirpath` only — no duplicate engine logic.

## aitest

```go
h := aitest.NewHarness(t, aitest.Options{
    SeedPatients:            true,
    WithSearch:              true,
    AllowPatientRead:        true,
    AllowPatientSearch:      true,
    AllowPatientSummaryView: true,
})

// h.Executor, h.Resources, h.Audit, h.Approval, h.Deid are ready for assertions
```

Configuration is option-based: enable only the subsystems each test needs.

## Migration

Existing helpers remain in place for incremental adoption:

| Legacy location | testkit replacement |
|-----------------|---------------------|
| `pkg/sync/helpers_test.go` | `synctest`, `storetest` |
| `pkg/store/store_test.go` | `storetest` |
| `pkg/ai/fixtures_test.go` | `aitest`, `fixtures` |
| `pkg/conflict/helpers_test.go` | `conflicttest`, `factories` |

No production package should gain a dependency on `pkg/testkit`.

## Where it fits

| Package | Role |
|---------|------|
| **types** | `ResourceEnvelope`, JSON codec, hashing |
| **store** | Interface contracts implemented by `storetest` |
| **jobs** | `InMemoryJobStore` reused by `storetest.Backend` |
| **sync** | Hub/engine exercised by `synctest` |
| **conflict** | Engine exercised by `conflicttest` |
| **fhirpath** | Engine wrapped by `fhirpathtest` |
| **ai** | Executor wired by `aitest` |
| **testkit** | Shared test support (this tree) |

## Package docs

Each subpackage has a `doc.go` with godoc-oriented API notes. Start with
[doc.go](./doc.go) for the tree overview.
