# haistack-testkit (`pkg/testkit`)

Shared importable test support for HAIStack: FHIR fixtures, in-memory store
fakes, sync and conflict scenario runners, OperationOutcome golden helpers,
FHIRPath assertions, and AI executor harnesses.

**This package is for tests only.** Production code must not import `pkg/testkit` or
any subpackage.

## How it fits in the ecosystem

```text
  pkg/* tests (_test.go)
        |
        v
  pkg/testkit/<subpackage>
        |
   +----+----+----+----+----+----+----+
   |    |    |    |    |    |    |    |
   v    v    v    v    v    v    v    v
fixtures store synctest conflict golden fhirpath aitest authztest
   |    test              |                    infernotest
   |                      |
   v                      v
 pkg/types            pkg/sync / pkg/conflict
 pkg/store            pkg/ai / pkg/auth (authz scenarios)
 pkg/fhirpath
```

| Direction | Package | Relationship |
|-----------|---------|--------------|
| Exercises | **store** | `storetest` implements interface contracts |
| Exercises | **sync** | `synctest` fakes `Hub` and device push/pull |
| Exercises | **conflict** | `conflicttest` builds two-node stale-base paths |
| Exercises | **ai** | `aitest` wires `Executor` with policy fakes |
| Exercises | **auth** | `authztest` catalogs REST/view/AI/sync decisions |
| Peer | **types** | Fixtures/factories emit `ResourceEnvelope` |
| Forbidden | **production** | No import from non-`_test` packages |

Testkit **consolidates duplicated `_test` helpers**; it does not ship runtime behavior or durable storage.

## Usage modes

### 1. Fixture-first scenarios

Stable ids and envelopes across packages — prefer `fixtures` when tests must align:

```go
patient := fixtures.PatientJane(t)
appt := fixtures.AppointmentForPatient(t, patient.ID)
```

### 2. Parameterized factories

Use `factories` when ids, telecom, status, or meta tags vary per case:

```go
patient, err := factories.NewPatient(
    factories.WithPatientID("pat-custom"),
    factories.WithFamilyName("Smith"),
)
```

### 3. In-memory store contract tests

`storetest.Backend` bundles resources, history, search, events, jobs:

```go
backend := storetest.NewStrictBackend()
_ = backend.Resources.Seed(ctx, patient)
```

Choose **strict** vs **lenient** `ResourceStore` to match core vs sync semantics.

### 4. Sync and offline replication scenarios

`synctest.Scenario` coordinates hub + device clocks and push/pull summaries:

```go
scenario := synctest.NewScenario("tenant-a", synctest.FixedClock(synctest.At(2026, 7, 6, 12, 0, 0)))
result, err := synctest.OfflineCreateAndSync(ctx, scenario, patient)
```

### 5. Conflict classification and merge paths

`conflicttest` evaluates concurrent edits and auto-merge vs review-required:

```go
result, err := conflicttest.NewScenario("tenant-a", clock).RunTwoNodeStaleBaseConflict(ctx, edits)
```

### 6. HTTP and validation goldens

Compare canonical `OperationOutcome` JSON without brittle string contains:

```go
outcome := golden.DecodeOutcome(t, body)
golden.AssertOutcomeCode(t, outcome, "not-found")
```

### 7. AI executor harness

`aitest` enables only the subsystems under test (search, views, approval):

```go
h := aitest.NewHarness(t, aitest.Options{
    SeedPatients: true,
    WithSearch:   true,
    AllowPatientRead: true,
})
res, err := h.Executor.ExecuteTool(ctx, req)
```

### 8. Authorization matrices (Go + YAML)

Run the full catalog or load machine-readable policy-semantics fixtures:

```go
authztest.RunAll(t, authztest.NewDefaultKit(authztest.DefaultEngine(t)))
authztest.RunYAML(t, yamlBytes)
```

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
| **authztest** | Authorization scenario catalog (≥30 Go cases) plus YAML catalogues (`ParseYAML` / `ScenariosFromYAML`) that assert SMART scope ∩ policy |
| **infernotest** | Inferno STU2 discovery + standalone SMART launch helpers and reference host |

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

    "github.com/degoke/haistack/pkg/testkit/fixtures"
    "github.com/degoke/haistack/pkg/testkit/synctest"
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

## authztest

Documented authorization scenario catalog for CI (REST, patient compartment,
SMART token semantics, view/AI/sync/module paths, scope-vs-policy conflicts):

```go
import "github.com/degoke/haistack/pkg/testkit/authztest"

func TestAuthzScenarios(t *testing.T) {
    eng := authztest.DefaultEngine(t)
    authztest.RunAll(t, authztest.NewDefaultKit(eng))
}
```

`AllScenarios()` returns ≥30 named cases with `Doc` strings suitable for
conformance matrices. OAuth success is not tested — only authorization outcomes.

Machine-readable YAML catalogues (used by `research/policy-semantics`) load
through `ParseYAML` / `ScenariosFromYAML` and run with `RunYAML` (no Go
`BaseConfig` kit). Every YAML principal must declare `tenant` and `kind`;
every scenario must declare `principal`, `scopes`, `action`, `resourceType`,
and `policy`. Omitting them is an error, not a fallback to `TenantA` / `user` /
`clinician` / `user/*.read` / `read` / `Patient` / `base`. Those cases assert
`SMART.ScopeImplies ∩ pkg/auth policy`. YAML view/AI actions call
`CanExecuteView` / `CanExecuteAITool` on the auth engine; they do not
load ViewDefinitions or run `pkg/view` / `pkg/ai` executors.

## More examples

**FHIRPath assertions on fixtures:**

```go
eng := fhirpathtest.DefaultEngine(t)
patient := fixtures.PatientJane(t)
fhirpathtest.AssertString(t, eng, patient, "Patient.name.family", "Doe")
fhirpathtest.AssertContains(t, eng, patient, "Patient.telecom.value", "555")
```

**Reference resolution after sync:**

```go
resolved, err := synctest.ReferenceResolved(ctx, scenario.DeviceB, appt,
    "participant.0.actor", "Patient", "pat-jane")
if err != nil || !resolved {
    t.Fatal("expected participant to resolve to hub patient")
}
```

**Inferno-oriented SMART host (tests only):**

```go
srv, meta, cleanup, err := infernotest.StartReferenceServer(ctx, ":0")
defer cleanup()
infernotest.AssertStandaloneLaunchFlow(t, meta.BaseURL, meta.FHIRBaseURL, "haistack-app", redirectURI, scope)
```

**Table-driven store tests with paged IDs:**

```go
backend := storetest.NewDeviceBackend()
_ = backend.Resources.Seed(ctx, fixtures.PatientJane(t))
ids, err := backend.Resources.ListIDs(ctx, "Patient", 10, 0)
```

## Testing

```bash
go test ./pkg/testkit/... -count=1
```

Subpackages are tested independently; importing only what a test needs keeps compile times down.

## Migration

Existing helpers remain in place for incremental adoption:

| Legacy location | testkit replacement |
|-----------------|---------------------|
| `pkg/sync/helpers_test.go` | `synctest`, `storetest` |
| `pkg/store/store_test.go` | `storetest` |
| `pkg/ai/fixtures_test.go` | `aitest`, `fixtures` |
| `pkg/conflict/helpers_test.go` | `conflicttest`, `factories` |

No production package should gain a dependency on `pkg/testkit`.

## Limits

- Test-only; must not be imported from production `pkg/*` (except tests)
- Fakes mirror store contracts but omit some edge-case SQL semantics
- Scenario catalogs (`authztest`) are synthetic, not clinical fixtures

## Related docs

- [doc.go](./doc.go) — tree overview
- Subpackage `doc.go` files (`authztest`, `synctest`, …) — focused APIs
- [pkg/store/README.md](../store/README.md) — contracts implemented by fakes

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

Each subpackage has a `doc.go` with godoc-oriented API notes (also linked above under Related docs).
