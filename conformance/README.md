# HAIStack conformance

FHIR Shorthand (FSH) is the source of truth for HAIStack capability definitions.
SUSHI compiles those files into FHIR JSON. `pkg/modules` installs the compiled
artefacts, and CI validates valid and invalid examples with the built-in Go
validator (`make validate-ig`).

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
| Go validator (`pkg/validate`) | in-tree |
| Node | 20 |

`make conformance-lock` records the git commit of the current checkout in the
lock file. Hosts should pin a release tag (or that SHA) at deploy time and
refuse to install `modules/*/ig` artefacts that were not produced from the same
conformance build.

## Commands

```bash
make ig              # npm ci, sushi --snapshot, export into modules/*/ig
make validate-ig     # Go validator on examples/valid and examples/invalid
make conformance-lock
```

Requirements: Node 20+, Go 1.22+, network on first run (npm, HL7 core package).
Subsequent runs reuse `conformance/node_modules` and `~/.fhir/packages`.

## Authoring

1. Edit FSH under `conformance/fsh/`.
2. Run `make ig`.
3. Commit both the FSH change and the regenerated `modules/*/ig` JSON.
4. Add or update examples when a profile constraint changes.
5. CI fails if FSH and exported IG artefacts drift, or if a valid example starts
   failing / an invalid example starts passing.

A breaking profile change — for example requiring `Patient.gender` on
`hai-patient` — fails `make validate-ig` until examples are updated.

## Runtime and IG validation

`pkg/validate` enforces installed StructureDefinitions on write and powers
`make validate-ig` in full validation mode (base HL7 R4 profile + declared
`meta.profile` URLs, slicing, SD terminology bindings, extension policy).

Runtime API writes use fast mode by default. Use `ValidationModeFull` or
`haistack validate --full` for certification-style depth.

## Out of scope

R5/R6 IGs, Touchstone/Inferno, and publication to packages.fhir.org.
