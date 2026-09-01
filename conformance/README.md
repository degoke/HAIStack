# HAIStack conformance

FHIR Shorthand (FSH) is the source of truth for HAIStack capability definitions.
SUSHI compiles those files into FHIR JSON. `pkg/modules` installs the compiled
artefacts, and CI runs the HL7 FHIR Validator against valid and invalid examples.

## Layout

```text
conformance/
  fsh/                      # FSH source (profiles, search parameters, terminology)
    core/
    sdc/
  input/fsh -> ../fsh       # created at build time for SUSHI
  examples/
    valid/                  # must pass the HL7 validator
    invalid/                # must fail, with *.expected.json sidecars
  terminology/              # pinned expansion parameters
  sushi-config.yaml
  package.json
  module-map.json           # which generated resources belong to which module
  scripts/
    build-ig.sh
    validate-ig.sh
    write-lock.sh
```

Compiled resources land in `conformance/fsh-generated/resources/` and are copied
into `modules/<name>/ig/` according to `module-map.json`. Those `ig/` directories
are what `pkg/modules` installs at runtime.

## Toolchain

Pinned in [`conformance-lock.json`](../conformance-lock.json) and
`conformance/package.json`:

| Tool | Version |
| --- | --- |
| FHIR | R4 4.0.1 |
| SUSHI / FSH | 3.20.0 |
| HL7 FHIR Validator | 6.9.11 |
| Node | 20 |

`make conformance-lock` records the git commit of the current checkout in the
lock file. Hosts should pin a release tag (or that SHA) at deploy time and
refuse to install `modules/*/ig` artefacts that were not produced from the same
conformance build.

## Commands

```bash
make ig              # npm ci, sushi --snapshot, export into modules/*/ig
make validate-ig     # HL7 validator on examples/valid and examples/invalid
make conformance-lock
```

Requirements: Node 20+, Java 17+ (validator), network on first run (npm, HL7
core package, validator JAR). Subsequent runs reuse `conformance/node_modules`,
`~/.fhir/packages`, and `conformance/.tools/`.

## Authoring

1. Edit FSH under `conformance/fsh/`.
2. Run `make ig`.
3. Commit both the FSH change and the regenerated `modules/*/ig` JSON.
4. Add or update examples when a profile constraint changes.
5. CI fails if FSH and exported IG artefacts drift, or if a valid example starts
   failing / an invalid example starts passing.

A breaking profile change — for example requiring `Patient.gender` on
`hai-patient` — fails `make validate-ig` until examples are updated.

## Runtime profile checks

`pkg/validate` can enforce installed StructureDefinitions on write. Opt in with
a profile catalog built from compiled IG JSON (or the definition store after
module install):

```go
catalog, err := validate.LoadProfileCatalogFromDir("modules/core/ig")
eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog})
svc, err := core.NewResourceService(core.ResourceServiceConfig{
    Validator: validate.NewCoreValidator(eng, validate.ValidateOptions{
        EnforceDeclaredProfiles: true,
    }),
})
```

This checks cardinality from the profile snapshot or differential. It is not a
replacement for the HL7 validator (no slicing, full FHIRPath invariants, or
terminology server). Use `make validate-ig` for certification-grade checking.

## Out of scope

R5/R6 IGs, Touchstone/Inferno, and publication to packages.fhir.org.
