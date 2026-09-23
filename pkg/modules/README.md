# haistack-modules (`pkg/modules`)

Manifest-driven capability modules for HAIStack: install FHIR definitions, enable
resource types, and record provenance without loading Go plugins.

Import path: `github.com/degoke/haistack/pkg/modules`.

---

## What it does

A **module** is a local directory with `module.json` plus optional FHIR definition
files or a compiled IG package directory. The installer:

1. Loads and validates the manifest (semver, dependencies, path safety).
2. Optionally verifies an Ed25519 signature over manifest + definition bytes.
3. Resolves the dependency graph (topological install order).
4. Seeds bundled definitions, enables declared resource types, ingests definitions
   into `store.DefinitionStore` with module provenance.
5. Records installation in `store.ModuleStore` and `CompletePackageInstall` in
   `store.RegistryInstallStore` under `registry.ModulesPackageID(name)`.

v1 modules are **manifests plus JSON** — no arbitrary module code execution.
Core and SDC IGs are authored in FSH under `conformance/fsh/` and installed from
compiled output in `modules/*/ig` (`make ig`).

Public API centers on **`Manager`**: `PlanInstall`, `Install`, `InstallAll`, `Upgrade`,
`Uninstall`, `List`, `Inspect`.

---

## How it fits in the ecosystem

```text
modules/<name>/module.json
        │
        ▼
   modules.Manager
        │
        ├── registry.Manager (snapshots, resource enablement)
        ├── store.DefinitionStore (StructureDefinition, SearchParameter, …)
        ├── store.ModuleStore (installed module records)
        ├── store.RegistryInstallStore (package completion)
        └── store.ResourceStore (uninstall safety: persisted resources)

pkg/runtime ──► WithModules / haistack.yaml modulePaths → InstallAll at startup
pkg/http ──► Basic/$install async module install jobs
pkg/terminology ──► global CodeSystems from IG packages (__global__ scope)
pkg/view ──► views[] declarations (metadata only in v1)
```

| Consumer | Uses modules for |
|----------|------------------|
| **runtime** | Declarative startup installs |
| **registry** | Enabled types + search params snapshot |
| **terminology** | Packaged CodeSystems/ValueSets |
| **sdc** | `modules/sdc` profiles, extensions, operations |

Repository examples: [`modules/core`](../../modules/core), [`modules/scheduling`](../../modules/scheduling),
[`modules/sdc`](../../modules/sdc).

---

## When to use it

- **Enable FHIR types** beyond bare R4 — Patient, Appointment, custom profiles
- **Ship search parameters** and conformance artifacts with semver upgrades
- **Reproducible installs** — plan before mutate, compensating transactions on failure
- **Signed modules** — untrusted sources with `Ed25519ModuleVerifier`

Do not use modules for application business logic (no plugin hooks). Deferred manifest
fields (`views`, `aiTools`, `permissions`, `syncPolicies`, `subscriptions`, `migrations`)
are recorded as metadata only in v1.

---

## Module layout

Package code: `pkg/modules`. Module directories: repository `modules/`:

```text
modules/<name>/
├── module.json
├── module.json.sig          # optional Ed25519 signature
├── ig/                      # compiled IG JSON (core, sdc)
└── definitions/             # optional hand-authored JSON
    └── *.json
```

---

## Manifest

```json
{
  "name": "scheduling",
  "version": "1.0.0",
  "description": "Scheduling resources and search parameters.",
  "dependencies": [
    {"name": "core", "version": "1.0.0"}
  ],
  "resources": ["Appointment", "Schedule", "Slot"],
  "definitionFiles": ["definitions/Appointment-search-date.json"],
  "igPackage": "ig",
  "views": ["AppointmentCalendar"],
  "aiTools": [],
  "permissions": ["read-appointment"],
  "syncPolicies": [],
  "subscriptions": [],
  "migrations": []
}
```

| Field | Purpose |
| --- | --- |
| `name` | Unique module name |
| `version` | Semver module version |
| `dependencies` | Required modules (minimum compatible version) |
| `resources` | Base FHIR resource types to enable |
| `definitionFiles` | Module-relative JSON paths (must stay inside module dir) |
| `igPackage` | Directory of compiled IG JSON (all `*.json` loaded) |
| `views`, `aiTools`, … | Capability declarations (metadata in v1) |

Loader rejects duplicate dependencies, bad semver, escaped paths, oversize manifests,
and symlinks pointing outside the module directory (`loader_test.go`).

---

## Usage modes

### 1. CLI (configured runtime)

```bash
haistack module plan modules/core
haistack module install modules/core
haistack module upgrade modules/core
haistack module list
haistack module inspect core
haistack module uninstall core --force
```

`module plan` previews dependencies, resource enablement, and definition changes
without mutating registry state.

### 2. Go `Manager`

```go
registryManager := registry.NewManager(registry.Config{
    Definitions: definitionStore,
    Installs:    installStore,
})

manager := modules.NewManager(modules.Config{
    ModuleStore:          moduleStore,
    DefinitionStore:      definitionStore,
    RegistryInstallStore: installStore,
    RegistryManager:      registryManager,
    ResourceStore:        resourceStore,
    Now:                  time.Now,
})

plan, err := manager.PlanInstall(ctx, "modules/scheduling")
result, err := manager.Install(ctx, "modules/scheduling")
```

`InstallAll` applies several paths as one compensating operation (runtime startup).
Failed multi-store operations roll back partial registry and module state
(`TestManagerInstallRollsBackRegistryChangesOnApplyFailure`).

### 3. Runtime composition

```go
rt, err := runtime.New().
    WithSQLite("health.db").
    WithModules("modules/core", "modules/sdc").
    Build(ctx)
```

Or declare `runtime.modulePaths` in `haistack.yaml`.

### 4. Signature verification

```go
manager := modules.NewManager(modules.Config{
    // ... stores ...
    Verifier: modules.Ed25519ModuleVerifier{PublicKey: publicKey},
})
```

Signature covers exact manifest bytes and every referenced definition file in
manifest order. Verification runs before dependency resolution or registry mutation.

### 5. HTTP async install

`POST /fhir/Basic/$install` enqueues local module install jobs; poll
`GET /fhir/Basic/{jobId}/$status`. Jobs stamp tenant ownership for terminology opt-in
when applicable.

---

## Lifecycle

```text
Load and validate module
        │
Optional signature verification
        │
Resolve dependency graph
        │
Build install or upgrade plan
        │
Optional InstallAuthorizer
        │
Apply registry changes and persist module state
        │
CompletePackageInstall(haistack-modules/<name>, version)
```

- **`Upgrade`** requires a greater semver; v1 upgrades are additive — removing a
  previously declared resource or definition returns `ErrUpgradeWouldRemove`.
- **`Uninstall`** is blocked when another module depends on the target or when
  persisted resources would lose an enabled type (`ErrResourcesExist`).

---

## Examples from this repo

**Install core** (`manager_test.go` `TestManagerInstallCoreEnablesResources`):

```go
result, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "core"))
// result.Name == "core", enables Organization, Patient, Practitioner
// Snapshot includes Patient search param "identifier-custom"
```

**Scheduling after core** (`TestManagerInstallSchedulingAfterCore`):

```go
_, _ = mgr.Install(ctx, ".../modules/core")
result, err := mgr.Install(ctx, ".../modules/scheduling")
// Enables Appointment, Schedule, Slot; search param "date-custom"
```

**Missing dependency** (`TestManagerInstallClinicalLiteFailsWithoutCore`):

```go
_, err := mgr.Install(ctx, ".../modules/clinical-lite")
// err wraps modules.ErrMissingDependency
```

**IG package loading** (`loader_test.go` `TestLoaderReadsIGPackage`, `TestLoadDefinitionsFromIG`).

**Authorizer hook** (`TestManagerInstallCallsAuthorizer`, `TestManagerInstallDeniedByAuthorizer`).

---

## Package install completion

Module installs record completion in `PackageInstallStore` under
`registry.ModulesPackageID(<moduleName>)` (for example `haistack-modules/core`) and
the manifest `version`. `CompletePackageInstall` runs after all definitions are ingested.
Provenance fields: `ModuleName`, `SourceModule` on definition records.

---

## Current scope (v1)

Active:

- Resource enablement and FHIR definition installation
- Dependencies, semver checks, upgrades, uninstall safety, optional signatures

Deferred (declarations persisted only):

- View registration, AI tools, permissions, sync policies, subscriptions, migrations
- Remote registry sync, tenant-specific module layers, paid modules

---

## Limits

- v1 executes registry enablement and definition install only; manifest arrays for views/AI/sync are stored but not applied automatically
- No remote module marketplace in this package (use `pkg/packages` for NPM IGs)
- Signature verification is optional; see README signature section for threat model

## Related docs

- [pkg/registry/README.md](../registry/README.md) — snapshots and enablement
- [pkg/runtime/README.md](../runtime/README.md) — startup wiring
- [modules/sdc](../../modules/sdc) — SDC module bundle
- [doc.go](./doc.go) — complete API
