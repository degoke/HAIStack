# haistack-conformance (`pkg/conformance`)

Go helpers for **Implementation Guide example validation** in CI and local checks—not a runtime FHIR server component.

## What it does

`pkg/conformance` connects the repository **conformance tree** (FSH-authored definitions and golden examples) to the in-process validator in [`pkg/validate`](../validate/README.md). It is the library behind `make validate-ig` and integration tests that assert IG quality gates.

Primary entry points:

- **`DefaultIGValidatorConfig(repoRoot)`** — Standard directory layout under a repo root (base R4 bundle, SUSHI output, valid/invalid examples).
- **`ValidateIG(ctx, cfg)`** — Load merged profile catalog, compile terminology from IG JSON, build `validate.Engine`, then:
  - **Valid examples** — Every `*.json` in `ValidDir` must pass **full** validation (`ValidationModeFull`, base + declared profiles, terminology).
  - **Invalid examples** — Every example JSON paired with a `*.expected.json` sidecar must **fail** validation against a declared profile, with issues matching the sidecar (substrings, codes, or FHIRPath expressions).

Supporting helpers:

- **`RepoRoot()`** — Walk parents from cwd until `go.mod` is found (used by tests).
- **`loadProfileCatalog`** — Merge base bundle tree + IG resources directory via `validate.LoadProfileCatalogFromDirTree` / `MergeProfileCatalogs`.
- **`loadTerminology`** — Scan IG resources for `CodeSystem` and `ValueSet`, compile into an in-memory `terminology.LocalService` scoped to `"conformance"`.

The **`conformance/`** directory at the repository root is where IG content is **authored** (FSH, SUSHI config, examples). This package is how Go code **loads and validates** that output without shelling to the HL7 Java validator.

## How it fits in the ecosystem

| Area | Location | Role |
|------|----------|------|
| Authoring | `conformance/fsh`, `conformance/scripts` | FSH, SUSHI, `make ig` |
| Generated IG JSON | `conformance/fsh-generated/resources` | Profiles, extensions, terminology |
| Examples | `conformance/examples/valid`, `.../invalid` | Golden pass/fail fixtures |
| **CI loader** | **`pkg/conformance`** | Catalog merge + `ValidateIG` |
| Runtime server | `registry`, `validate`, `core` | Production validation without importing this package |

Contributors edit FSH, run **`make ig`** to regenerate resources, then **`make validate-ig`** (or `go test ./pkg/conformance/...`) to enforce example contracts.

Application servers at runtime use `registry` snapshots and `validate.Engine` directly; they only need this package if you embed the same IG example suite in a fork or custom test harness.

## When to use it

Use `pkg/conformance` when you need to:

- **Gate CI** on valid examples passing and invalid examples failing predictably.
- **Reuse the same paths and catalog merge** as Makefile targets in Go tests.
- **Extend the IG** with new invalid fixtures that document expected validator diagnostics via sidecars.

Do **not** use this package to compile FSH (use `make ig` / `conformance/scripts/build-ig.sh`) or to validate live traffic in production (use `validate` with your deployed registry snapshot).

Skip IG validation tests locally only when generated resources are missing (`TestValidateIGExamples` skips with `"run make ig"`).

## Usage modes

### 1. Repository Makefile (recommended for contributors)

From repo root:

```bash
make ig
make validate-ig
```

These targets invoke the same layout as `DefaultIGValidatorConfig`.

### 2. Full IG validation in Go (`ValidateIG`)

```go
root, err := conformance.RepoRoot()
if err != nil {
    return err
}
cfg := conformance.DefaultIGValidatorConfig(root)
if err := conformance.ValidateIG(ctx, cfg); err != nil {
    return err
}
```

On success, stdout includes lines like `PASS valid example.json` and `all IG examples matched expected validator outcomes`.

### 3. Integration test pattern (`conformance_test`)

```go
func TestValidateIGExamples(t *testing.T) {
    root, err := conformance.RepoRoot()
    if err != nil {
        t.Fatal(err)
    }
    igDir := filepath.Join(root, "conformance/fsh-generated/resources")
    if _, err := os.Stat(igDir); err != nil {
        t.Skip("IG resources missing; run make ig")
    }
    if err := conformance.ValidateIG(context.Background(), conformance.DefaultIGValidatorConfig(root)); err != nil {
        t.Fatal(err)
    }
}
```

### 4. Sidecar contract tests

Invalid fixtures require paired expectations (`TestInvalidExampleSidecarsRequireMustFail`):

```go
// Each *.expected.json must contain: "mustFail": true
```

Sidecars also require `"profile"` for invalid examples; matching uses `expectedSubstrings`, `expectedCodes`, or `expectedExpressions`.

### 5. Custom config paths

For forks or alternate monorepo layouts, construct `IGValidatorConfig` manually:

```go
cfg := conformance.IGValidatorConfig{
    BaseBundleRoot: "/path/to/r4/bundle",
    IGResourcesDir: "/path/to/ig/resources",
    ValidDir:       "/path/to/examples/valid",
    InvalidDir:     "/path/to/examples/invalid",
}
err := conformance.ValidateIG(ctx, cfg)
```

Missing directories return errors that mention **`run make ig first`**.

### 6. Terminology-only compilation path

`loadTerminology` is internal but behavior matters: CodeSystems and ValueSets under `IGResourcesDir` compile into the validator's `Terminology` service for binding checks during full validation.

## Examples (from source and tests)

**Default paths** (`catalog.go`):

```go
func DefaultIGValidatorConfig(repoRoot string) IGValidatorConfig {
    return IGValidatorConfig{
        BaseBundleRoot: filepath.Join(repoRoot, "pkg/registry/internal/bundles/r4"),
        IGResourcesDir: filepath.Join(repoRoot, "conformance/fsh-generated/resources"),
        ValidDir:       filepath.Join(repoRoot, "conformance/examples/valid"),
        InvalidDir:     filepath.Join(repoRoot, "conformance/examples/invalid"),
    }
}
```

**Valid example validation options** (`ig.go`):

```go
validate.ValidateOptions{
    EnforceBaseProfile:      true,
    EnforceDeclaredProfiles: true,
    ProfileConstraints:      true,
    Mode:                    validate.ValidationModeFull,
    Terminology:             term,
}
```

**Invalid example sidecar shape** (`expectedExample` in `ig.go`):

```json
{
  "mustFail": true,
  "profile": "http://example.org/fhir/StructureDefinition/MyProfile",
  "expectedSubstrings": ["required element"],
  "expectedCodes": ["required"],
  "expectedExpressions": ["Patient.name"]
}
```

**Reading an example envelope**:

```go
env, err := readEnvelope(path) // *types.ResourceEnvelope with ResourceType + JSON
result, err := engine.Validate(ctx, env, opts)
```

## Configuration / key types

| Type / function | Purpose |
|-----------------|--------|
| `IGValidatorConfig` | `BaseBundleRoot`, `IGResourcesDir`, `ValidDir`, `InvalidDir` |
| `DefaultIGValidatorConfig(repoRoot)` | HAIStack standard paths |
| `ValidateIG(ctx, cfg) error` | Run full valid + invalid suite |
| `RepoRoot() (string, error)` | Find repo root via `go.mod` |
| `expectedExample` (unexported) | Sidecar schema for invalid tests |
| `fullValidationOptions` | Valid directory validation |
| `profileOnlyValidationOptions` | Invalid directory, single profile |

Validator engine configuration uses `validate.NewEngine(validate.Config{ProfileCatalog, FHIRPath})` with a default `fhirpath.NewEngine(fhirpath.Config{})`.

## Where it fits

```text
conformance/fsh  ──SUSHI/make ig──►  conformance/fsh-generated/resources
                                              │
conformance/examples/valid|invalid            │
              │                               │
              └──────────►  pkg/conformance.ValidateIG
                                    │
                                    ├── merge(base R4 bundle, IG profiles)
                                    ├── terminology from IG CodeSystem/ValueSet
                                    └── validate.Engine
                                              │
                                              ▼
                                    PASS/FAIL per example + sidecars
```

Runtime FHIR traffic does not flow through this package; it validates **checked-in examples** against **checked-in definitions**.

## Limits

- **No FSH compilation** — Must run `make ig` before validation; empty `fsh-generated` causes skip or directory errors.
- **Validator feature parity** — Behavior follows [`pkg/validate`](../validate/README.md), not every feature of the HL7 official validator or HAPI.
- **Invalid matching** — Sidecars match substrings, issue codes, or expressions heuristically; brittle diagnostics text can break tests if messages change.
- **Terminology scope** — In-memory terminology is built from IG JSON in `IGResourcesDir`; external terminology servers are not used here.
- **Production** — Passing `ValidateIG` does not replace deployment-specific regression tests against your live registry snapshot.

## Related docs

- [conformance/README.md](../../conformance/README.md) — authoring guide at repo root
- [Root README — Conformance section](../../README.md)
- [pkg/validate/README.md](../validate/README.md)
- [pkg/registry/README.md](../registry/README.md) — bundled R4 base bundle path
- [pkg/packages/README.md](../packages/README.md) — installing NPM IGs vs local FSH output
