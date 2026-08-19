# Registry

Shared FHIR definition catalog for the monorepo.

## Scope

- Bundled HL7 FHIR R4 base `StructureDefinition` and `SearchParameter` resources
- Persistent catalog storage via `pkg/store`
- SQLite and Postgres backends (`pkg/sqlite`, `pkg/postgres`)
- In-memory compiled snapshot for fast lookups
- Resource enablement overlay
- Capability snapshot for future HTTP consumers

## Usage

```go
manager := registry.NewManager(registry.Config{
    Definitions: db.DefinitionStore(),
    Installs:    db.RegistryInstallStore(),
})
_ = manager.SeedBundled(ctx)
_ = manager.InstallDefinitionsFromDir(ctx, "/path/to/custom/definitions", registry.InstallProvenance{
    PackageName: "example.custom",
})
_ = manager.EnableResource(ctx, "Patient")
snapshot, _ := manager.RebuildSnapshot(ctx)
```

`SeedBundled` only loads the embedded HL7 bundle. Use `InstallDefinitionsFromDir` or `InstallDefinitionsFromFS` for custom definition JSON trees on disk.

Postgres uses a hybrid model: the base catalog is global on `postgres.DB`; enablement lives on `postgres.TenantDB`.

## Bundled catalog

Base definitions are vendored under `internal/bundles/r4/` from HL7 `definitions.json.zip`:

```bash
make generate-r4-bundle
```

This writes 148 base resource `StructureDefinition` files and 1375 base `SearchParameter` files.

## Snapshot

`Snapshot` implements `validate.ResourceTypeRegistry` and exposes:

- enabled resource checks
- base structure definitions
- search parameter metadata and expressions
- canonical lookup
- empty profile and operation buckets reserved for future compilation

Unknown future definition kinds can be stored through `InstallDefinition` and are ignored by the MVP compiler.
