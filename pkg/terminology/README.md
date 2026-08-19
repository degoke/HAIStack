# `haistack-terminology` (`pkg/terminology`)

Tenant-scoped terminology lookup, validation, and finite ValueSet expansion
for FHIR R4.

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

## Scope and lifecycle

The canonical identity is:

```text
scope_id + canonical_url + version
```

SQLite uses the configured local scope; Postgres uses the tenant ID. Historical
or retired versions remain readable when explicitly requested, but retired
versions are excluded from current-version resolution.

There are intentionally no HTTP `$lookup`, `$expand`, or `$validate-code`
routes yet. The internal service and storage contracts are the stable first
release boundary.
