# haistack-packages (`pkg/packages`)

FHIR **NPM package** installer for the HAIStack registry catalog.

## What it does

`pkg/packages` downloads and ingests packages from **[packages.fhir.org](https://packages.fhir.org)** (or a custom registry base), extracts tarball contents, and loads **StructureDefinitions, SearchParameters, CodeSystems**, and related definition JSON into [`pkg/registry`](../registry/README.md). It can enable declared resource types and optionally register **ViewDefinitions** when `ViewRegistry` and FHIRPath engine are configured.

This complements [`pkg/modules`](../modules/README.md):

| Mechanism | Source | Typical use |
|-----------|--------|-------------|
| `pkg/modules` | Local `module.json` directory in your repo | First-party capability bundles (`modules/core`, `modules/sdc`) |
| `pkg/packages` | Published NPM packages (`hl7.fhir.r4.core`, IGs) | Upstream HL7 or vendor IGs |

Startup helpers in this package wire install specs into runtime initialization (see `startup.go`).

## When to use it

- **Seed standard R4 definitions** beyond the bundled snapshot  
- **Install a published IG** (profiles + search params) before enabling resources  
- **CI or server bootstrap** — declarative list of package id + version  

Use local `modules` when you author definitions in-repo via FSH ([conformance/README.md](../../conformance/README.md)).

## Usage

### Programmatic install from registry

```go
import (
    "context"

    "github.com/degoke/haistack/pkg/packages"
    "github.com/degoke/haistack/pkg/registry"
)

inst := &packages.Installer{
    Registry:     reg, // *registry.Manager
    Refresh:      func(ctx context.Context) error { _, err := reg.RebuildSnapshot(ctx); return err },
    RegistryBase: "https://packages.fhir.org",
}
res, err := inst.InstallFromRegistry(ctx, "hl7.fhir.r4.core", "4.0.1")
```

### Local directory

```go
res, err := inst.InstallFromDirectoryVersion(ctx, "/path/to/package", "my.ig", "1.0.0")
```

### Startup specs

```go
specs := []packages.InstallSpec{
    {PackageID: "hl7.fhir.r4.core", Version: "4.0.1"},
}
err := packages.InstallConfigured(ctx, inst, specs)
```

Progress callbacks (`OnProgress`) report long downloads/extracts.

## Where it fits

```text
packages.fhir.org ──► pkg/packages.Installer ──► pkg/registry (definitions + snapshot)
                                                          │
                                                          ▼
                                                    pkg/core / validate / search
```

Runtime may call startup install before `SeedBundled` or immediately after, depending on your builder configuration.

## Limits

- Signature verification options exist—see installer tests and comments for your threat model.  
- Installing huge IGs increases snapshot size and validation cost—install only packages you enable in production.  
- ViewDefinition registration requires explicit `ViewRegistry` wiring; definitions alone do not create analytics tables until `pkg/view` / `pkg/analytics` consume them.

## Related docs

- [pkg/modules/README.md](../modules/README.md)  
- [pkg/registry/README.md](../registry/README.md)
