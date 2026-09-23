# haistack-registry (`pkg/registry`)

Shared FHIR definition catalog for the HAIStack monorepo.

## What it does

**haistack-registry** is the **definition catalog and compile layer** for FHIR R4. It ingests `StructureDefinition`, `SearchParameter`, and other conformance resources, persists them through `store.DefinitionStore`, tracks which resource types are **enabled** via `store.RegistryInstallStore`, and builds an in-memory **`Snapshot`** for fast lookups at runtime.

The central type is **`Manager`**: seed embedded HL7 base content, install custom packages from disk or `fs.FS`, enable or disable resource types, optionally project `CodeSystem` / `ValueSet` into terminology stores, enqueue ValueSet pre-expand jobs, and rebuild the compiled view after changes.

**`Snapshot`** implements `validate.ResourceTypeRegistry` and exposes structure definition JSON, search parameter metadata (code, type, FHIRPath expression), canonical definition lookup, patient-scoping helpers, and a **`CapabilitySnapshot`** suitable for future HTTP capability statements.

The package also ships **read-only embedded fallbacks** (`EmbeddedDefinitionStore`, `DefinitionStoreWithEmbeddedBase`) so callers can resolve base HL7 structure definitions even when the durable catalog row is absent.

## What it does not do

- **Parse or validate instance FHIR JSON** — use `pkg/types` and `pkg/validate`
- **Persist resources or run SQL** — use `pkg/store` contracts and `pkg/sqlite` / `pkg/postgres`
- **Execute FHIR search or build index rows** — use `pkg/search` (which *reads* registry snapshots)
- **Serve HTTP `$metadata` or package registry APIs** — future API layers consume `Snapshot` / `CapabilitySnapshot`
- **Replace terminology compilation logic** — `CodeSystem` / `ValueSet` installs delegate to `pkg/terminology`

Registry owns **what definitions are installed and enabled**, not clinical data or query execution.

## How it fits in the ecosystem

```
  HL7 R4 bundle (embedded)     NPM / module JSON trees
           |                              |
           v                              v
    Manager.SeedBundled()     InstallDefinitionsFromFS/Dir()
           \                            /
            v                          v
         store.DefinitionStore  +  store.RegistryInstallStore
                    |
                    v
            Manager.RebuildSnapshot()
                    |
                    v
                 Snapshot  ----------------------+
                    |                          |
         +----------+----------+               |
         v          v          v               v
    pkg/validate  pkg/search  pkg/core    CapabilitySnapshot
    (types)       (index)     (writes)    (future HTTP)
         |
         v
  optional: TerminologyStore, JobStore (pre-expand), SearchReindexNotifier
```

| Direction | Package | Relationship |
|-----------|---------|--------------|
| Upstream | **store** | `DefinitionStore`, `RegistryInstallStore`, `TerminologyInstallStore`, `PackageInstallStore`, `JobStore` contracts |
| Upstream | **terminology** | Install/projection for CodeSystem and ValueSet; eligibility for pack pre-expand |
| Upstream | **jobs** | `EnqueuePackPreExpand` after package install when configured |
| Downstream | **search** | `SnapshotRegistry`, search expressions, reindex notifiers |
| Downstream | **validate** | `Snapshot` as `ResourceTypeRegistry` |
| Downstream | **core** | Enabled resource checks before writes |
| Sidecar | **sqlite** / **postgres** | Durable definition and install persistence |

Postgres uses a **hybrid model**: the base definition catalog is global on `postgres.DB`; per-tenant **enablement** lives on `postgres.TenantDB`. SQLite typically colocates both on one `sqlite.DB`.

## When to use it

- Bootstrapping a server or test database with HL7 R4 base definitions (`SeedBundled`)
- Installing IG or custom NPM package JSON trees with provenance (`InstallDefinitionsFromDir` / `InstallDefinitionsFromFS`)
- Turning resource types on or off without deleting catalog rows (`EnableResource` / `DisableResource`)
- Supplying search parameter expressions and structure definitions to `pkg/search` and `pkg/validate`
- Resolving patient scope for indexing or authorization (`PatientSearchParameterCode`, `PatientReferenceResolver`)
- Checking whether a package version finished install (`PackageVersionInstalled`)
- Opting terminology packs in per tenant during background jobs (`ContextWithJobTerminologyInstalls`)

## Usage modes

### Durable catalog with SQLite or Postgres

Open a backend, construct a manager from store adapters, seed bundled content, enable resources, and compile:

```go
import (
    "context"

    "github.com/degoke/haistack/pkg/registry"
    "github.com/degoke/haistack/pkg/sqlite"
)

ctx := context.Background()
db, err := sqlite.Open(sqlite.Options{Path: "data.db"})
if err != nil {
    return err
}

manager := registry.NewManager(registry.Config{
    Definitions: db.DefinitionStore(),
    Installs:    db.RegistryInstallStore(),
})
if err := manager.SeedBundled(ctx); err != nil {
    return err
}
if err := manager.EnableResource(ctx, "Patient"); err != nil {
    return err
}
snapshot, err := manager.RebuildSnapshot(ctx)
if err != nil {
    return err
}
_ = snapshot.IsResourceEnabled("Patient")
```

### Install a custom definition package from disk

Walk every `.json` file under a directory, ingest with package provenance, then complete the package (pre-expand + completion marker when configured):

```go
err := manager.InstallDefinitionsFromDir(ctx, "/path/to/package/package", registry.InstallProvenance{
    PackageName:    "example.custom",
    PackageVersion: "1.0.0",
    SourceModule:   "example.custom",
})
```

`InstallDefinitionsFromFS` is the same pipeline for any `fs.FS` root. `InstallDefinition` ingests a single JSON resource; call `CompletePackageInstall` yourself when batching manual installs.

### Embedded base fallback without seeding

For read-only resolution of HL7 base structure definitions when the primary store misses:

```go
defs := registry.DefinitionStoreWithEmbeddedBase(db.DefinitionStore())
rec, err := defs.Get(ctx, "http://hl7.org/fhir/StructureDefinition/Patient", "4.0.1")
```

Or use `registry.EmbeddedDefinitionStore()` alone in tests that only need bundled R4 structure definitions.

### Terminology, jobs, and search reindex hooks

Wire optional dependencies on `registry.Config` so installs side-effect terminology projection, ValueSet pre-expand, and search reindex:

```go
manager := registry.NewManager(registry.Config{
    Definitions:         db.DefinitionStore(),
    Installs:            db.RegistryInstallStore(),
    Terminology:         tenantDB.TerminologyStore(),
    GlobalTerminology:   globalDB.TerminologyStore(),
    TerminologyScope:    tenantID,
    TerminologyInstalls: tenantDB.TerminologyInstallStore(),
    JobStore:            tenantDB.JobStore(),
    PackageInstalls:     db.PackageInstallStore(),
    PreExpandValueSets:  true,
    SearchReindex:       reindexNotifier, // implements registry.SearchReindexNotifier
})
```

`EnsureTerminologyPackEnabled` opts a pack into the terminology install overlay without reinstalling definitions.

## Examples

**Snapshot lookups (search indexing and capability):**

```go
snapshot, err := manager.RebuildSnapshot(ctx)
if err != nil {
    return err
}
expr, ok := snapshot.SearchExpression("Patient", "family")
cap := snapshot.CapabilitySnapshot()
_ = expr
_ = ok
_ = cap.FHIRVersion
```

**Install, delete, and package completion:**

```go
raw, _ := os.ReadFile("custom-search-parameter.json")
err := manager.InstallDefinition(ctx, raw, registry.InstallProvenance{
    ModuleName: "my-module", SourceModule: "my-module",
    PackageName: "my-module", PackageVersion: "1.0.0",
})
err = manager.DeleteDefinition(ctx, canonicalURL, version)
done, err := manager.PackageVersionInstalled(ctx, "example.custom", "1.0.0")
_ = done
```

**Patient reference resolution:**

```go
resolver := &registry.PatientReferenceResolver{}
resolver.SetSnapshot(snapshot)
patientID, ok, err := resolver.PatientIDForResource(ctx, "Observation", envelope)
_ = patientID
_ = ok
_ = err
```

**Parse metadata without persisting; module package id:**

```go
parsed, targets, err := registry.ParseDefinition(jsonData)
pkgID := registry.ModulesPackageID("observations")
_ = parsed.DefinitionKind
_ = targets
_ = pkgID
```

**Regenerate embedded HL7 bundle (maintainers):**

```bash
make generate-r4-bundle
```

Writes 148 base `StructureDefinition` and 1375 `SearchParameter` JSON files under `internal/bundles/r4/`.

## Configuration / key types

### `registry.Config`

| Field | Role |
|-------|------|
| `Definitions` | Required durable catalog (`store.DefinitionStore`) |
| `Installs` | Resource enablement overlay (`store.RegistryInstallStore`) |
| `FHIRVersion` | Defaults to `4.0.1` (`DefaultFHIRVersion`) |
| `Now` | Clock for install timestamps (tests) |
| `SearchReindex` | Optional `SearchReindexNotifier` / `SearchParameterReindexNotifier` |
| `Terminology` / `GlobalTerminology` | Projection targets; CodeSystem/ValueSet route to global when set |
| `TerminologyScope` | Scope id for tenant terminology store |
| `TerminologyInstalls` | Opt-in overlay for terminology packs |
| `TerminologyCache` | `terminology.Invalidator` on install/delete |
| `JobStore` | Enqueue pack pre-expand when `PreExpandValueSets` is true |
| `PackageInstalls` | Mark package versions complete after `CompletePackageInstall` |
| `PreExpandValueSets` | Gate ValueSet pre-expand enqueue (skipped for `BundledCorePackageName`) |

**`InstallProvenance`:** `PackageName` / `PackageVersion` (NPM identity), `ModuleName` (ownership; conflicts → `ErrDefinitionConflict`), `SourceModule` (auto registry install rows when set).

**Constants:** `BundledCorePackageName` (`hl7.fhir.r4.core`), `ModulesPackageName` (`haistack-modules`), `DefaultFHIRVersion` (`4.0.1`).

**`Snapshot`:** `EnabledResourceTypes`, `IsResourceEnabled` / `IsInstalled`, `StructureDefinitionFor`, `SearchParametersFor` / `SearchParameter` / `SearchExpression`, `DefinitionsByCanonical` / `AnyDefinitionByCanonical`, `PatientSearchParameterCode`, `CapabilitySnapshot`; `ProfilesFor` / `Operations` reserved (empty in MVP).

**Errors:** `ErrInvalidDefinition`, `ErrMissingDefinition`, `ErrSnapshotCompile`, `ErrDefinitionConflict`, `ErrResourceNotEnabled`, `ErrDefinitionNotFound`.

## Where it fits

| Package | Role |
|---------|------|
| **store** | Persistence contracts for definitions, installs, terminology installs, package installs, jobs |
| **registry** | Catalog ingest, compile, snapshot (this package) |
| **search** | Indexing and query planning from snapshot search parameters |
| **validate** | Structure validation using `Snapshot` as type registry |
| **terminology** | CodeSystem/ValueSet install and pre-expand eligibility |
| **jobs** | Pack pre-expand job type `registry.terminology.pre_expand_valuesets` |
| **sqlite** / **postgres** | Backend implementations |
| **modules** | Module-owned definitions via `ModuleName` provenance |

## Limits

- **MVP compiler** ingests `StructureDefinition` (resource kind) and `SearchParameter` fully; other FHIR definition types may be **stored** via `InstallDefinition` but are **ignored** when building `Snapshot` search/structure maps.
- **`ProfilesFor` and `Operations`** return empty slices today; buckets exist for future compilation.
- **`SeedBundled`** is idempotent and **never overwrites** existing rows with the same canonical URL/version; it does not enqueue ValueSet pre-expand for the bundled core package.
- **Cross-module installs** of the same canonical URL/version fail with `ErrDefinitionConflict` when `ModuleName` differs.
- **`CompileSnapshot`** fails with `ErrSnapshotCompile` when an enabled resource lacks a valid base structure definition or search parameter JSON cannot be parsed.
- **Patient scoping** prefers single-base search parameters; multi-base parameters (for example cross-resource `clinical-patient`) may rank lower for `PatientSearchParameterCode`.
- **Chained embedded store** `Get` falls through to embedded HL7 structure definitions only; list/upsert/delete operate on the primary store.
- **Reindex scheduling** requires injecting `SearchReindex`; without it, enable/install still persists but search indexes are not automatically rebuilt.

## Related docs

- [docs/architecture.md](../../docs/architecture.md) — monorepo layering
- [docs/overview.md](../../docs/overview.md) — product overview
- [pkg/store/README.md](../store/README.md) — definition and install store contracts
- [pkg/search/README.md](../search/README.md) — registry-driven search indexing
- [pkg/validate/README.md](../validate/README.md) — validation against registry types
- [pkg/terminology/README.md](../terminology/README.md) — terminology projection and pre-expand
- [pkg/jobs/README.md](../jobs/README.md) — background job runtime (pre-expand enqueue)
- [pkg/sqlite/README.md](../sqlite/README.md) / [pkg/postgres/README.md](../postgres/README.md) — durable backends

See also bundled catalog maintenance: `make generate-r4-bundle` and `internal/bundles/r4/`.
