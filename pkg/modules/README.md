# haistack-modules (`pkg/modules`)

`pkg/modules` installs manifest-driven capability modules into a HAIStack
runtime. A module is a local directory containing a `module.json` manifest and
optional FHIR definition files. The installer validates the module, checks its
dependencies, enables declared resource types, imports definitions into the
registry, and records the installation through backend-neutral store
interfaces.

The package does not load Go plugins or execute arbitrary module code. In v1,
modules are manifests plus JSON definitions. Core and SDC definitions are
authored in FSH under `conformance/fsh/` and installed from compiled IG output
in `modules/*/ig` (`make ig`).

## Module Layout

The package lives at `pkg/modules`; module directories are normally kept under
the repository-level `modules/` directory:

```text
modules/<name>/
├── module.json
├── ig/                 # compiled IG JSON (core and sdc)
└── definitions/        # optional hand-authored JSON
    └── *.json
```

For example, [`modules/core`](../../modules/core) enables foundational FHIR
resources and installs compiled IG artefacts from `ig/` (built from
[`conformance/fsh`](../../conformance/fsh)). [`modules/scheduling`](../../modules/scheduling)
still uses hand-authored `definitions/`.

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
| `name` | Unique module name. |
| `version` | Semver module version. |
| `description` | Human-readable description. |
| `dependencies` | Required modules and their minimum compatible versions. |
| `resources` | Base FHIR resource types to enable. |
| `definitionFiles` | Module-relative JSON definitions to ingest with provenance. |
| `igPackage` | Module-relative directory of compiled IG JSON (all `*.json` files are loaded). |
| `views`, `aiTools`, `permissions`, `syncPolicies`, `subscriptions`, `migrations` | Capability declarations recorded as metadata but not executed by v1. |

Definition paths must remain inside the module directory. The loader also
validates manifest structure, rejects duplicate entries, and applies size and
file-count limits.

## Lifecycle

`Manager` is the runtime-facing API:

```text
Load and validate module
        |
Optional signature verification
        |
Resolve dependency graph
        |
Build install or upgrade plan
        |
Optional authorization
        |
Apply registry changes and persist module state
```

The main operations are:

- `PlanInstall` previews an install or upgrade without changing state.
- `Install` installs a module and can apply a newer version of an existing module.
- `InstallAll` applies several module paths as one compensating operation.
- `Upgrade` explicitly upgrades an installed module.
- `Uninstall` removes a module and its registry contributions when safe.
- `List` returns installed modules and their contributions.
- `Inspect` returns one installed module.

Installs seed bundled FHIR definitions before enabling resources. Definition
records retain module provenance, and the registry snapshot is rebuilt after the
operation. Failed multi-store operations are compensated so partial registry
and module state is not intentionally left behind.

Upgrades must use a greater version and are additive in v1. Removing a
previously declared resource or definition is rejected with
`ErrUpgradeWouldRemove`. Uninstall is blocked when another module depends on
the target or when removing a resource would leave persisted resources without
an enabled type.

## CLI

The `haistack` CLI exposes the module lifecycle when a configured runtime is
available:

```bash
haistack module plan modules/core
haistack module install modules/core
haistack module upgrade modules/core
haistack module list
haistack module inspect core
haistack module uninstall core --force
```

`module plan` is useful for checking dependencies, resource enablement, and
definition changes before installation.

## Go Usage

Construct the manager with the stores used by the rest of the runtime. SQLite
and Postgres provide concrete implementations of these `pkg/store` contracts.

```go
import (
    "context"
    "time"

    "github.com/degoke/health-ai-stack/pkg/modules"
    "github.com/degoke/health-ai-stack/pkg/registry"
)

// Construct these from the selected persistence backend.
var moduleStore store.ModuleStore
var definitionStore store.DefinitionStore
var installStore store.RegistryInstallStore
var resourceStore store.ResourceStore

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

ctx := context.Background()
plan, err := manager.PlanInstall(ctx, "modules/scheduling")
if err != nil {
    // Handle invalid manifests, missing dependencies, or registry conflicts.
}
_ = plan

result, err := manager.Install(ctx, "modules/scheduling")
if err != nil {
    // Handle the install error.
}
_ = result
```

The `store` import is omitted above only for brevity; the store variables are
typically supplied by `pkg/sqlite` or `pkg/postgres`.

For runtime composition, `pkg/runtime` wires a `modules.Manager` and can call
`InstallAll` for configured module paths during startup.

## Signature Verification

Deployments that install modules from an untrusted source can configure a
`ModuleVerifier`. The built-in `Ed25519ModuleVerifier` expects a detached
`module.json.sig` next to the manifest by default:

```go
manager := modules.NewManager(modules.Config{
    ModuleStore:          moduleStore,
    DefinitionStore:      definitionStore,
    RegistryInstallStore: installStore,
    RegistryManager:      registryManager,
    Verifier: modules.Ed25519ModuleVerifier{
        PublicKey: publicKey,
    },
})
```

The signature covers the exact manifest bytes and every referenced definition
file, including its manifest-order path framing. Verification happens before
dependency resolution or registry mutation.

## Current Scope

v1 is registry-first:

- Resource enablement and FHIR definition installation are active.
- Module dependencies, semver checks, upgrades, uninstall safety, and optional
  signatures are supported.
- View, AI tool, permission, sync policy, subscription, and migration entries
  are persisted as declarations only.
- Remote registry synchronization, tenant-specific module layers, paid modules,
  and execution of deferred subsystem declarations are not implemented yet.

See [`doc.go`](./doc.go) for the complete package-level API and behavior notes.
