# haistack-conformance (`pkg/conformance`)

Go helpers for **IG example validation** in CI—not a runtime FHIR server component.

## What it does

`pkg/conformance` wires paths under the repository **conformance tree** into [`pkg/validate`](../validate/README.md) profile catalogs:

- Base R4 bundle profiles (`pkg/registry/internal/bundles/r4`)  
- FSH-generated IG resources (`conformance/fsh-generated/resources`)  
- Golden **valid** and **invalid** example JSON (`conformance/examples/valid|invalid`)  

`DefaultIGValidatorConfig(repoRoot)` returns the standard layout used by `make validate-ig` and workflow jobs.

The **`conformance/`** directory at repo root (FSH, SUSHI, examples) is where definitions are **authored**; this package is how Go tests and CI **load** them.

## When to use it

- **CI validation** — ensure valid examples pass and invalid examples fail with expected diagnostics  
- **Custom test harnesses** — reuse the same catalog merge as the Makefile targets  
- **Contributors** — after editing FSH, run `make ig` then `make validate-ig`  

Application servers at runtime use `registry` + `validate` directly; they do not need to import `pkg/conformance` unless you embed the same CI checks in your fork.

## Usage

```go
import (
    "github.com/degoke/haistack/pkg/conformance"
    "github.com/degoke/haistack/pkg/validate"
)

cfg := conformance.DefaultIGValidatorConfig("/path/to/repo")
// Pass cfg into IG validation helpers used in conformance/ig_test.go patterns
_ = validate.Engine{} // see pkg/validate and conformance/scripts
```

Prefer running repository targets from the root:

```bash
make ig
make validate-ig
```

## Where it fits

```text
conformance/fsh ──SUSHI──► modules/*/ig + fsh-generated
                                    │
                                    ▼
pkg/conformance ──► validate.Engine ──► examples/valid|invalid
```

## Limits

- Does not compile FSH—that is `make ig` / `conformance/scripts/build-ig.sh`.  
- Validator mode and profile coverage follow [`pkg/validate`](../validate/README.md) capabilities, not every HL7 validator feature.

## Related docs

- [conformance/README.md](../../conformance/README.md) (authoring guide)  
- [Root README — Conformance section](../../README.md#conformance-fsh--ig--validator)
