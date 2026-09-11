# `haistack-terminology` (`pkg/terminology`)

Tenant-scoped terminology lookup, validation, and finite ValueSet expansion
for FHIR R4 with a platform-wide global CodeSystem catalog and per-tenant
ValueSet overlays.

## Design

FHIR `CodeSystem` and `ValueSet` JSON is the source of truth. The terminology
package owns parsing and compilation; database packages only implement the
projection contract in `pkg/store`.

```text
canonical FHIR JSON
        │
        ├── terminology_resource / terminology_valueset
        │
        └── compiled projections
             ├── CodeSystem concepts
             └── ValueSet expansion members
```

The projections are disposable. `terminology.Install` replaces a resource's
projection, while `terminology.Rebuild` reconstructs all projections in a
scope from canonical terminology JSON.

## Basic usage

```go
import (
    "context"
    "github.com/degoke/health-ai-stack/pkg/store"
    "github.com/degoke/health-ai-stack/pkg/terminology"
)

ctx := context.Background()
termStore := terminology.NewMemoryStore() // use db.TerminologyStore() in production

codeSystem := []byte(`{"resourceType":"CodeSystem","url":"urn:example:sex","version":"1","status":"active","concept":[{"code":"female","display":"Female"}]}`)
err := terminology.Install(ctx, termStore, store.TerminologyResourceRecord{
    ScopeID: "clinic-a", ResourceType: "CodeSystem",
    CanonicalURL: "urn:example:sex", Version: "1", Status: "active",
    ResourceJSON: codeSystem,
})
```

For database-backed installs, use `store.TerminologyResourceRecord` from
`pkg/store` and the backend's terminology store. Resource-service writes and
module installation already compile projections in their transaction when the
backend supports a terminology write session.

```go
svc := terminology.NewLocalService(db.TerminologyStore(), "default")

result, err := svc.Lookup(ctx, terminology.LookupRequest{
    System: "urn:example:sex", Version: "1", Code: "female",
})
```

`Compile` writes projections only. `Install` normalizes canonical JSON, calls
`Compile`, and persists the resource record via `TerminologyStore.PutResource`.
Use `Install` for normal ingest; call `Compile` directly only when the canonical
record is already stored.

`LocalService` supports:

- exact CodeSystem URL/version/code lookup;
- deterministic current-version selection when version is omitted;
- direct and CodeSystem-based ValueSet composition;
- nested ValueSet inclusion and finite expansion pagination;
- explicit Coding validation and CodeableConcept validation;
- display mismatch warnings without rejecting a valid code.

`Chain` provides provider precedence. Providers are evaluated in order, so a
tenant-local provider can precede module, built-in, or remote providers.

## Validation integration

Terminology checks are opt-in. Supply a terminology service and explicit
bindings through `validate.ValidateOptions`:

```go
result, err := engine.Validate(ctx, resource, validate.ValidateOptions{
    Terminology:        svc,
    TerminologyEnabled: true,
    TerminologyBindings: map[string]validate.TerminologyBinding{
        "code": {URL: "urn:example:sex", Version: "1", Strength: "required"},
    },
})
```

Unknown terminology is distinct from an invalid code. Unavailable providers
are warnings rather than invalid codes, and display mismatches are warnings.

## Global vs tenant scoping

CodeSystems and packaged ValueSets installed from IG packages are stored under
`terminology.GlobalScopeID` (`__global__`) and shared across tenants.
Tenant-custom ValueSets remain in the tenant scope. Tenants opt in to global
catalog entries via `TerminologyInstallStore`. `LayeredStore` composes tenant
overlays with opted-in global CodeSystems and ValueSets:

```text
tenant LocalService → global LocalService → RemoteProvider (optional)
```

Configure a remote terminology server at runtime with
`runtime.Builder.WithRemoteTerminology("https://tx.fhir.org/r4")`. The remote
provider supports FHIR R4 `$lookup`, `$expand`, and `$validate-code` with
in-memory caching, request throttling, and a simple circuit breaker.

Per-tenant opt-in records live in `TerminologyInstallStore` (parallel to
`RegistryInstallStore`). The **installing tenant** is auto-opted-in when a
package or module installs terminology (`Enabled: true`; `sourceModule` records
the package or module). Passive install/restart paths use `EnsureInstallOptIn`
and never override explicit opt-out (`enabled=false` from
`$terminology-enable`). Other tenants must still call
`POST /fhir/Basic/$terminology-enable` (single URL or whole pack via `packName`).
At server startup, configured installs use the default/sync tenant (SQLite
`sqliteTenantID`, Postgres tenant DB).

Server startup can install local modules and FHIR packages declaratively via
`haistack.yaml` (`runtime.modulePaths`, `runtime.packages`). Installs are
idempotent: completed package versions are tracked in `PackageInstallStore`
(`CompletePackageInstall` records the first completion only; partial installs
resume on restart). Databases upgraded before this table existed re-run install
once on the next startup so `CompletePackageInstall` can record completion.
Global terminology is not re-compiled when the resource already exists in `__global__`.
Re-installing a completed package version only opts in absent terminology rows
for the installing tenant.

Pack-level enable validates every entry against the global catalog. Entries
missing from `__global__` are skipped and returned as `warning` parameters;
check `count` and `warning` in the response — `count: 0` with warnings is
success, not an error, when every pack entry is absent from the global catalog.

## Scope and lifecycle

The canonical identity is:

```text
scope_id + canonical_url + version
```

SQLite uses the configured local scope; Postgres uses the tenant ID for
ValueSets and `__global__` for shared CodeSystems. Historical or retired
versions remain readable when explicitly requested, but retired versions are
excluded from current-version resolution.

HTTP terminology operations are exposed when `TerminologyService` is wired:

- `CodeSystem/$lookup`
- `ValueSet/$expand`
- `CodeSystem/$validate-code` and `ValueSet/$validate-code`

Platform FHIR operations:

- `POST /fhir/ImplementationGuide/$install` — async IG/package install
- `POST /fhir/Basic/$install` — async local module install
- `GET /fhir/Basic/{jobId}/$status` — poll background job status and progress; internal registry jobs stamp `principalId: "registry"` (synthetic owner, not a real user principal)
- `POST /fhir/CapabilityStatement/$refresh` — hot-reload conformance state
- `POST /fhir/Basic/$terminology-install` — rebuild projections; optional `preExpandValueSets=true`
- `POST /fhir/Basic/$terminology-enable` — opt a tenant into a global CodeSystem or ValueSet, or a whole pack via `packName`/`packVersion` (requires global terminology store; validates catalog entries)

## Optional ValueSet pre-expansion

Finite packaged ValueSets can be pre-expanded at install time (opt-in via
`haistack.yaml` `runtime.preExpandValueSets`, `runtime.Builder.WithPreExpandValueSets(true)`, or
`Basic/$terminology-install?preExpandValueSets=true`). Registry package install
enqueues one `registry.terminology.pre_expand_valuesets` job per package version
(not per ValueSet) when at least one eligible ValueSet exists; bundled R4 core
(`hl7.fhir.r4.core`) is excluded. REST definition writes via the FHIR API call
`CompletePackageInstall` once per package version after bundle sync. Re-install
resets a completed pack job to pending; duplicate pending jobs are skipped.
Pre-expand jobs require `packName` and `packVersion` payload fields.
Pre-expand skips ValueSets that already ship `expansion.contains`, already have
matching `ExpansionFingerprint` members, exceed `MaxExpansion`, use unbounded
compose heuristics (`ShouldEnqueuePreExpand`), or reference CodeSystems the
tenant has not opted into. Runtime `$expand` prefers stored members and only
composes when the projection is empty.
