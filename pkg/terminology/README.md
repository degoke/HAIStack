# haistack-terminology (`pkg/terminology`)

Tenant-scoped terminology lookup, validation, and finite ValueSet expansion for
FHIR R4 — with a platform-wide global CodeSystem catalog and per-tenant ValueSet overlays.

Import path: `github.com/degoke/haistack/pkg/terminology`.

---

## What it does

FHIR `CodeSystem` and `ValueSet` JSON is the **source of truth**. This package owns
parsing, compilation into queryable projections, and service APIs (`LocalService`,
`Chain`, remote providers). Database backends implement `store.TerminologyStore`; they
do not embed terminology logic.

| Operation | API | Notes |
|-----------|-----|-------|
| Ingest | `Install`, `Compile` | Normalize JSON → projections |
| Lookup | `LocalService.Lookup` | URL/version/code → display |
| Expand | `Expand` | Finite compose → member list |
| Validate | `ValidateCode`, `ValidateCoding` | Required bindings, display warnings |
| Translate | `Translate` | ConceptMap-aware (`conceptmap_translate_test.go`) |
| Subsumes | `Subsumes` | Walk CodeSystem parent links |
| Rebuild | `Rebuild` | Reconstruct projections for a scope |

Projections are disposable: `Install` replaces a resource's projection; `Rebuild`
reconstructs all projections in a scope from canonical terminology JSON.

---

## Design

Canonical FHIR JSON flows through storage and compilation:

```text
canonical FHIR JSON (CodeSystem / ValueSet)
        │
        ├── terminology_resource / terminology_valueset records
        │
        └── compiled projections
             ├── CodeSystem concepts (parent links, properties)
             └── ValueSet expansion members (finite sets)
```

Identity key:

```text
scope_id + canonical_url + version
```

**Global vs tenant:** CodeSystems and packaged ValueSets from IG packages live under
`terminology.GlobalScopeID` (`__global__`) and are shared. Tenant-custom ValueSets
stay in the tenant scope. Tenants opt into global catalog entries via
`TerminologyInstallStore`.

**Layered resolution:**

```text
tenant LocalService → global LocalService → RemoteProvider (optional)
```

`Chain` evaluates providers in order so tenant-local entries precede module, built-in,
or remote fallbacks (`chain_test.go`).

Remote FHIR terminology (`RemoteProvider`): R4 `$lookup`, `$expand`, `$validate-code`
with caching, throttling, and circuit breaker. Configure via
`runtime.Builder.WithRemoteTerminology("https://tx.fhir.org/r4")`.

Opt-in remote gate tests (`remote_gate_test.go`) block global ValueSet expand without
tenant opt-in while allowing remote-only ValueSets when configured.

---

## How it fits in the ecosystem

```text
Module / Package install ──► terminology.Install (__global__ or tenant)
        │
        ▼
 store.TerminologyStore
        │
        ▼
 terminology.LocalService / LayeredStore / Chain
        │
        ├── pkg/validate (TerminologyBindings)
        ├── pkg/sdc (answerValueSet, open-choice)
        ├── pkg/cql (MemberOf, Subsumes)
        ├── pkg/structuremap (translate transform)
        └── pkg/fhirpath (memberOf() in views)

HTTP (when wired): CodeSystem/$lookup, ValueSet/$expand, $validate-code
Platform: Basic/$terminology-install, Basic/$terminology-enable, IG/$install
```

Resource-service writes and module installation compile projections inside their
transaction when the backend supports a terminology write session.

---

## When to use it

- **Validate codings** on resources with explicit bindings (`validate.ValidateOptions`)
- **Expand value sets** for SDC choice items and search filters
- **Package catalogs** — install once globally, opt tenants in per CodeSystem/ValueSet/pack
- **Hybrid** — local overlays + remote tx.fhir.org fallback via `Chain`

Use **`Compile` directly** only when the canonical record is already stored; normal
ingest should call **`Install`**.

---

## Usage modes

### 1. In-memory / tests

From `terminology_test.go` `TestLocalCodeSystemAndValueSet`:

```go
ctx := context.Background()
m := terminology.NewMemoryStore()

cs := []byte(`{"resourceType":"CodeSystem","url":"urn:test","version":"2",
  "concept":[{"code":"a","display":"Alpha"},{"code":"b","display":"Beta"}]}`)
_ = m.PutResource(ctx, store.TerminologyResourceRecord{
    ScopeID: "s", ResourceType: "CodeSystem",
    CanonicalURL: "urn:test", Version: "2", ResourceJSON: cs,
})
_ = terminology.Compile(ctx, m, "s", "", cs)

svc := &terminology.LocalService{Store: m, ScopeID: "s"}
got, err := svc.Lookup(ctx, terminology.LookupRequest{System: "urn:test", Code: "a"})

vs := []byte(`{"resourceType":"ValueSet","url":"urn:vs","version":"1",
  "compose":{"include":[{"system":"urn:test","concept":[{"code":"a"}]}]}}`)
_ = terminology.Compile(ctx, m, "s", "", vs)
ex, err := svc.Expand(ctx, terminology.ExpandRequest{URL: "urn:vs"})
```

Display mismatch returns `Valid` with `DisplayWarning: true` (not a hard error).

### 2. Production install

```go
err := terminology.Install(ctx, termStore, store.TerminologyResourceRecord{
    ScopeID: "clinic-a", ResourceType: "CodeSystem",
    CanonicalURL: "urn:example:sex", Version: "1", Status: "active",
    ResourceJSON: codeSystemJSON,
})

svc := terminology.NewLocalService(db.TerminologyStore(), "default")
result, err := svc.Lookup(ctx, terminology.LookupRequest{
    System: "urn:example:sex", Version: "1", Code: "female",
})
```

### 3. Validation integration

```go
result, err := engine.Validate(ctx, resource, validate.ValidateOptions{
    Terminology:        svc,
    TerminologyEnabled: true,
    TerminologyBindings: map[string]validate.TerminologyBinding{
        "code": {URL: "urn:example:sex", Version: "1", Strength: "required"},
    },
})
```

Unknown terminology is distinct from invalid codes. Unavailable providers surface as
warnings rather than invalid codes.

### 4. Provider chain

```go
svc := terminology.Chain(
    terminology.NewLocalService(tenantStore, tenantID),
    terminology.NewLocalService(globalStore, terminology.GlobalScopeID),
    remoteProvider,
)
```

### 5. Tenant opt-in to global catalog

- Installing tenant is auto-opted-in when a package/module installs terminology.
- Other tenants: `POST /fhir/Basic/$terminology-enable` (URL or whole pack via `packName`).
- Pack-level enable skips entries missing from `__global__` with warning parameters;
  `count: 0` with warnings is success when every entry is absent globally.

`LayeredStore` composes tenant overlays with opted-in global CodeSystems and ValueSets
(`preexpand_test.go` layered store tests).

### 6. HTTP terminology operations

When `TerminologyService` is wired:

- `CodeSystem/$lookup`
- `ValueSet/$expand`
- `CodeSystem/$validate-code` and `ValueSet/$validate-code`

Platform operations:

- `POST /fhir/Basic/$terminology-install` — rebuild projections; `preExpandValueSets=true`
- `POST /fhir/Basic/$terminology-enable` — opt into global entries or packs
- `POST /fhir/ImplementationGuide/$install` — async IG install (terminology side effects)

---

## Optional ValueSet pre-expansion

Finite packaged ValueSets can be pre-expanded at install time:

- `haistack.yaml` `runtime.preExpandValueSets`
- `runtime.Builder.WithPreExpandValueSets(true)`
- `Basic/$terminology-install?preExpandValueSets=true`

Registry package install enqueues `registry.terminology.pre_expand_valuesets` per package
version when eligible ValueSets exist (bundled R4 core excluded). Pre-expand skips
ValueSets that already ship `expansion.contains`, match `ExpansionFingerprint`, exceed
`MaxExpansion`, fail `ShouldEnqueuePreExpand` heuristics, or reference CodeSystems the
tenant has not opted into (`preexpand_test.go`).

Runtime `$expand` prefers stored members; composes when the projection is empty.
Stale members are ignored when compose changes (`TestExpandIgnoresStaleMembersWhenComposeChanges`).

---

## Scope and lifecycle

- **Current version:** deterministic selection when version omitted; retired versions
  excluded from current resolution (`TestRetiredCurrentVersionExcluded`).
- **Historical versions:** readable when explicitly requested.
- **SQLite multi-tenant:** opt-in rows use `sqliteTenantID`; overlay scope defaults
  differ — authenticated HTTP uses principal `tenantID` for both opt-in and overlay
  (`TerminologyInstallStoreFactory.ForTenant`).

Package install idempotency: `PackageInstallStore` tracks completed versions;
re-install opts in absent terminology rows without re-compiling existing global resources.

---

## Examples from this repo

**Subsumes hierarchy** (`subsumes_test.go`):

```go
// LocalService.Subsumes walks CodeSystem parent links; breaks cycles safely
```

**ConceptMap translate + audit** (`conceptmap_translate_test.go`):

```go
// Translate uses ConceptMap resources; audit records resolved map identity
```

**Chain translate** (`chain_test.go` `TestChainHasTranslate`):

```go
// First provider with Translate wins
```

**Pre-expand pack filtering** (`preexpand_test.go` `TestPreExpandPackFiltersEligibleValueSets`).

---

## Limits

- Infinite or unbounded ValueSet compose may return expand-too-costly errors (`TestChainExpandTooCostlyIsTerminal`).
- Remote provider requires network egress policy allow-list when configured.
- Pre-expand jobs require `packName` and `packVersion` payload fields.
- Global terminology is not re-compiled when the resource already exists in `__global__`.

---

## Related docs

- [pkg/validate/README.md](../validate/README.md) — terminology bindings
- [pkg/store/README.md](../store/README.md) — TerminologyStore contract
- [pkg/conceptmap/README.md](../conceptmap/README.md) — translate operations
- [pkg/sdc/README.md](../sdc/README.md) — answer value set validation
- [doc.go](./doc.go) — API boundaries
