# haistack-packages (`pkg/packages`)

FHIR **NPM package** installer for pulling Implementation Guide and core definition tarballs into the HAIStack registry catalog.

## What it does

`pkg/packages` downloads, extracts, and ingests **FHIR NPM packages** (the same layout published to [packages.fhir.org](https://packages.fhir.org)). Each JSON definition with a `resourceType` and canonical `url` is parsed and passed to [`registry.Manager.InstallDefinition`](../registry/README.md). Successful installs record provenance (`PackageName`, `PackageVersion`, `SourceModule`) and call `CompletePackageInstall` so restarts can skip completed versions.

The installer can:

- **Download from a registry** — `GET {RegistryBase}/{packageId}/{version}` (default base `https://packages.fhir.org`).
- **Install from a local directory tree** — walk JSON definitions, skip `package.json`.
- **Install from a tarball stream** — gzip+tar extract with path traversal checks; prefers `package/` subdirectory when present.
- **Enable resource types** — when `EnableTypes` is true, StructureDefinitions with `kind: resource` trigger `Registry.EnableResource` for their `type`.
- **Register ViewDefinitions** — when `ViewRegistry` and `FHIRPath` engine are set, `ViewDefinition` resources call `view.RegisterViewDefinition`.
- **Refresh runtime** — optional `Refresh` callback (typically `reg.RebuildSnapshot`) after install.
- **Report progress** — `OnProgress(current, total, message)` for long installs; wired to job progress in `InstallWorker`.

Startup helpers (`InstallSpec`, `InstallConfigured`, `InstallIfNeeded`) support declarative bootstrap: skip installs already recorded as complete, but still ensure terminology packs stay enabled.

This package does **not** compile FSH or author IGs—that is the repo [`conformance/`](../../conformance/README.md) tree and [`pkg/modules`](../modules/README.md) for in-repo capability bundles.

## How it fits in the ecosystem

HAIStack loads conformance from three common sources:

| Mechanism | Package | Source | Typical content |
|-----------|---------|--------|-----------------|
| Bundled snapshot | `registry` | `pkg/registry/internal/bundles/r4` | Shipped R4 core |
| **NPM packages** | **`packages`** | packages.fhir.org or local tarball | `hl7.fhir.r4.core`, vendor IGs |
| Local modules | `modules` | `module.json` + repo tree | First-party `modules/core`, `modules/sdc` |

After definitions land in the registry, **validation** (`validate`), **search** (`search`), **core** REST behavior, and **terminology** packs consume the merged snapshot. ViewDefinitions installed here only become runnable analytics when [`pkg/view`](../view/README.md) and optional [`pkg/analytics`](../analytics/README.md) are configured.

Async installs integrate with the job system via `InstallWorker` and `jobs.PackageInstallPayload` (`registry` or `upload` source).

## When to use it

Use `pkg/packages` when you need to:

- **Seed or upgrade HL7 core** beyond the bundled snapshot (for example explicit `hl7.fhir.r4.core` version pins).
- **Install a published IG** before enabling custom profiles or SearchParameters in production.
- **Bootstrap CI or server startup** from a declarative list of package id + version (or local path + version).
- **Accept uploaded NPM tarballs** through the package install job worker.

Use **`pkg/modules`** when definitions live in your repository and you ship them as HAIStack modules, not as NPM tarballs.

Use **FSH / SUSHI** under `conformance/` when you author profiles locally; then install from `conformance/fsh-generated` via `InstallFromDirectoryVersion` or copy into a module tree.

## Usage modes

### 1. Registry download (`InstallFromRegistry`)

Download and install one version from packages.fhir.org (or custom `RegistryBase`):

```go
inst := &packages.Installer{
    Registry:     reg,
    Refresh:      func(ctx context.Context) error { _, err := reg.RebuildSnapshot(ctx); return err },
    RegistryBase: "https://packages.fhir.org",
}
res, err := inst.InstallFromRegistry(ctx, "hl7.fhir.r4.core", "4.0.1")
```

### 2. Local directory (`InstallFromDirectory` / `InstallFromDirectoryVersion`)

Install JSON from disk; id defaults to directory base name, version to `"local"` unless specified:

```go
res, err := inst.InstallFromDirectoryVersion(ctx, "/path/to/ig/package", "my.ig", "1.0.0")
```

Tests use a minimal IG with `DemoPatient` StructureDefinition (`installer_test.go`).

### 3. Archive stream (`InstallFromArchive`)

Supply tarball bytes (HTTP response body or upload file):

```go
res, err := inst.InstallFromArchive(ctx, "demo.ig", "1.0.0", reader)
```

Non-FHIR JSON and `package.json` are skipped (`TestInstallFromArchiveSkipsNonFHIRJSON`).

### 4. Idempotent startup (`InstallConfigured` / `InstallIfNeeded`)

Skip packages whose version is already complete; validate specs first:

```go
specs := []packages.InstallSpec{
    {PackageID: "hl7.fhir.r4.core", Version: "4.0.1"},
    {PackageID: "my.ig", Version: "1.0.0", Path: "/abs/path/to/package"},
}
err := packages.InstallConfigured(ctx, inst, specs)
```

`InstallSpec.Validate()` requires `packageId`+`version` for registry installs, or `path`+`version` for local paths. `Normalize()` absolutizes `Path` and derives `PackageID` from the basename when empty.

### 5. Background job worker (`InstallWorker`)

Server jobs with payload source `registry` or `upload` delegate to the same `Installer`, forwarding `OnProgress` to `jobs.Reporter`:

```go
worker := &packages.InstallWorker{Installer: inst, Store: jobStore, ...}
err := worker.HandleJob(ctx, job)
```

### 6. Enable types + register views

Production installers often set:

```go
inst.EnableTypes = true
inst.ViewRegistry = viewReg
inst.FHIRPath = fpEngine
```

StructureDefinitions enable REST resource types; ViewDefinitions register for SQL-on-FHIR style execution.

## Examples (from source and tests)

**Minimal directory install** (`TestInstallFromDirectorySkipsPackageJSON`):

```go
installer := &packages.Installer{Registry: testRegistryManager(t)}
result, err := installer.InstallFromDirectory(ctx, packageDir)
// result.Installed == 1 for one StructureDefinition JSON
```

**InstallResult fields**:

```go
// InstallResult: PackageID, Version, Installed (count), Enabled (resource types), ExtractedTo
```

**Registry manager test helper** (`installer_test.go`):

```go
reg := registry.NewManager(registry.Config{
    Definitions: db.DefinitionStore(),
    Installs:    db.RegistryInstallStore(),
})
```

**Startup idempotency** (`startup_test.go`) exercises `InstallIfNeeded` when `PackageVersionInstalled` is already true—returns `skipped=true` and ensures terminology pack enablement without re-downloading.

**Custom HTTP client or temp dir**:

```go
inst.HTTPClient = &http.Client{Timeout: 10 * time.Minute}
inst.TempDir = "/var/tmp/haistack-packages"
```

## Configuration / key types

| Field / type | Purpose |
|--------------|---------|
| `Installer` | Main entry: `Registry`, `Refresh`, `RegistryBase`, `HTTPClient`, `TempDir` |
| `EnableTypes` | Auto-`EnableResource` for SD `kind: resource` |
| `ViewRegistry`, `FHIRPath` | Register `ViewDefinition` resources |
| `OnProgress` | `ProgressFunc(current, total, message)` |
| `InstallResult` | Summary returned from install methods |
| `InstallSpec` | Declarative `{PackageID, Version, Path}` |
| `InstallSpec.Normalize`, `Validate` | Path absolutization and validation |
| `InstallConfigured` | Loop specs with error wrapping |
| `InstallIfNeeded` | Skip completed versions |
| `InstallWorker` | Job handler for async installs |
| `defaultRegistryBase` | `https://packages.fhir.org` when `RegistryBase` empty |

Install flow always calls `Registry.SeedBundled` before ingesting definitions so base R4 dependencies exist.

## Where it fits

```text
packages.fhir.org  (or local tarball / directory)
        │
        ▼
pkg/packages.Installer
        │  InstallDefinition (each JSON)
        │  CompletePackageInstall
        │  optional Refresh → RebuildSnapshot
        ▼
pkg/registry  (definitions DB + install records + snapshot)
        │
        ├──► pkg/validate / pkg/search / pkg/core
        └──► pkg/view (+ analytics) when ViewDefinitions registered
```

```text
HTTP/admin or jobs ──► InstallWorker ──► Installer (same paths as above)
```

Runtime order is deployment-specific: some builders call `InstallConfigured` before or after `SeedBundled`; the installer itself seeds bundled definitions on each install invocation.

## Limits

- **Threat model** — tarball extract rejects path escape; review registry trust and TLS for production downloads. Signature verification is not described as a default gate—treat package source integrity as an operational concern.
- **Snapshot size and startup** — large IGs increase memory for validation and search index build; install only packages you enable.
- **ViewDefinitions** — registration requires explicit `ViewRegistry` + FHIRPath; installing an IG does not create analytics tables until view/analytics layers consume those definitions.
- **Skipped files** — only JSON with both `resourceType` and `url` install; ancillary IG assets are ignored.
- **EnableResource errors** — missing dependency definitions may skip enable with `ErrMissingDefinition` rather than failing the whole package.
- **Refresh failures** — install may succeed but return error from `Refresh` callback; handle partial success in ops playbooks.

## Related docs

- [pkg/registry/README.md](../registry/README.md)
- [pkg/modules/README.md](../modules/README.md)
- [pkg/view/README.md](../view/README.md)
- [conformance/README.md](../../conformance/README.md) (authoring IGs locally)
- [pkg/conformance/README.md](../conformance/README.md) (CI example validation)
