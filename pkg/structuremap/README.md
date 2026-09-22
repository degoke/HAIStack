# haistack-structuremap (`pkg/structuremap`)

Minimal FHIR **StructureMap** execution engine for **SDC questionnaire extraction**.

## What it does

`pkg/structuremap` runs StructureMap **group rules** against `Questionnaire` and `QuestionnaireResponse` inputs and produces FHIR resources suitable for **transaction bundle** entries. Supported transforms cover common SDC extraction patterns (`create`, `copy`, `uuid`, `cc`, `evaluate`, and related helpers)—not full FHIR Mapping Language parity.

The entry group is selected by matching `StructureMap.name` to a group name; otherwise the first group runs. Helper groups invoke through rule dependents.

Integration with questionnaires is through [`pkg/sdc`](../sdc/README.md) (`ExtractorRun`, `NewExtractor`). [`pkg/conceptmap`](../conceptmap/README.md) backs `translate()` transforms.

## When to use it

- **QuestionnaireResponse → clinical resources** — Patient, Observation, Condition, etc.  
- **Custom extraction** — when SDC `sourceStructureMap` points at your StructureMap resource  
- **Tests and CI** — golden extraction without a full HAPI StructureMap engine  

Prefer manual mapping in application code only for trivial one-field extractions; StructureMap keeps extraction declarative and versionable as FHIR resources.

## Usage modes

### 1. Via SDC (recommended)

Use `POST /fhir/QuestionnaireResponse/$extract` or `sdc.Extractor` from a configured runtime. The default runtime adapter delegates to this package when `sourceStructureMap` is present.

```go
import "github.com/degoke/haistack/pkg/sdc"

// Extraction returns a transaction Bundle envelope; apply via core.ProcessTransactionBundle when ready.
```

See [pkg/sdc/README.md](../sdc/README.md) for HTTP routes and adapter injection.

### 2. Direct engine

Wire `structuremap.Config` with `Resolver` and `Engine`, then use `NewExtractor` for SDC or call `Engine.Execute` directly:

```go
import "github.com/degoke/haistack/pkg/structuremap"

cfg := structuremap.Config{
    Resolver: myResolver,
    Engine:   myEngine, // implements structuremap.Engine
}
extractor := structuremap.NewExtractor(cfg)
_ = extractor // sdc.StructureMapExtractor → runtime.WithSDC
```

See `pkg/structuremap/engine.go` and tests for `ExecuteInput` and group execution.

## Where it fits

```text
QuestionnaireResponse ──► pkg/sdc ($extract)
                              │
                              ▼
                         pkg/structuremap ──► Bundle entries ──► pkg/core (optional apply)
                              │
                              └──► pkg/conceptmap (translate)
                                   pkg/fhirpath (evaluate)
```

## Limits

- Not a general-purpose FHIR mapping platform—scope is SDC extraction paths exercised in this repo.  
- Unsupported transforms fail with explicit errors during rule execution.  
- Complex maps may require extending the engine; prefer keeping maps within the supported transform set documented in tests under `pkg/structuremap/`.

## Related docs

- [pkg/sdc/README.md](../sdc/README.md)  
- [modules/sdc/examples/](../../modules/sdc/examples/) — sample questionnaires and maps
