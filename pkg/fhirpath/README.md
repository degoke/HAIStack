# haistack-fhirpath (`pkg/fhirpath`)

In-memory FHIRPath evaluation for a **single FHIR resource at a time**.

## What it does

[FHIRPath](https://build.fhir.org/ig/HL7/FHIRPath/) is a small query language for FHIR — like XPath for XML. You write expressions such as:

- `Patient.name.given` — given names
- `Patient.telecom.where(system = 'phone').value` — phone numbers
- `Observation.value.ofType(Quantity).value` — a numeric observation value

This package **runs those expressions** against a Patient, Observation, or other resource you already have in memory. It does **not** search a database or fetch resources by itself; it only reads fields from a resource you pass in.

The implementation wraps [github.com/verily-src/fhirpath-go](https://github.com/verily-src/fhirpath-go) behind a stable haistack API (`Engine`, `CompiledExpression`, `Value`).

**Does not:** import `store`, `sqlite`, or `postgres`; evaluate across resource graphs without an optional `Resolve` backend; accept raw JSON maps as roots (use `ResourceEnvelope` or R4 proto).

## How it fits in the ecosystem

FHIRPath answers “what is inside this resource?” Search answers “which resources match?” ViewDefinitions project many resources into rows.

```
                    ┌─────────────────────────────────────┐
                    │  FHIR Search (pkg/search)           │
                    │  Registry SearchParameter.expression│
                    └──────────────┬──────────────────────┘
                                   │ index write path
                                   ▼
              RegistryIndexer ──► fhirpath.Eval ──► normalized index tokens
                                   │
     ViewDefinition where/select ──┼──► fhirpath (parse + execute)
                                   │
     SDC calculatedExpression ─────┼──► fhirpath (+ SDC custom fns)
                                   │
     Subscription FilterFHIRPath ──┘
```

| Consumer | Role of fhirpath |
|----------|------------------|
| **pkg/search** | `RegistryIndexer` evaluates SearchParameter expressions |
| **pkg/view** | ViewDefinition filters and column paths at parse and scan time |
| **pkg/sdc** | Questionnaire `text/fhirpath` items; `NewSDCFHIRPathEngine` adds `weight()` |
| **pkg/subscriptions** | `Matcher.FilterFHIRPath` on change events |
| **pkg/structuremap** | Shared engine for map-dependent logic |
| **cmd/haistack** | `haistack fhirpath eval` for local debugging |

## When to use it

- Extracting fields from a resource already loaded in memory (UI, validation, indexing)
- Compiling hot-path expressions once (`Compile`) and reusing across many resources
- Wiring search indexing when registry SearchParameters use FHIRPath expressions
- Parsing ViewDefinitions or SDC questionnaires that reference FHIRPath
- Optional: enabling `resolve()` or terminology functions when you configure resolvers

Prefer **`pkg/search`** for “find all Patients named Smith”, not FHIRPath over the whole database.

## Usage modes

### Mode: Direct evaluation (CLI or application)

One-shot or compiled evaluation against an envelope or R4 proto:

```go
eng, err := fhirpath.NewEngine(fhirpath.Config{})
ctx := context.Background()

values, err := eng.Eval(ctx, "Patient.name.given", envelope)
name, err := eng.EvalString(ctx, "Patient.name.given.first()", envelope)

compiled, _ := eng.Compile("Patient.telecom.where(system = 'phone').value")
phones, _ := compiled.Eval(ctx, envelope)
```

CLI: `haistack fhirpath eval patient.json 'Patient.name.family'`.

### Mode: Search indexing

```go
indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{
    Registry: search.NewSnapshotRegistry(snapshot),
    Engine:   fhirpathEngine,
})
```

Param matching: `search.MatchResourceParameter(ctx, reg, engine, "Observation", envelope, "code", wantTokens)`.

### Mode: ViewDefinition execution

```go
parser, _ := view.NewDefinitionParser(engine)
spec, _ := parser.Parse(viewDefinitionJSON)
reg := view.NewRegistry()
_, _ = reg.Register(spec, engine)
exec, _ := view.NewExecutor(view.Config{Resources: resourceStore, Engine: engine, Registry: reg})
result, _ := exec.Execute(ctx, view.ExecuteRequest{ViewName: spec.Name})
```

### Mode: SDC questionnaires

```go
engine, err := sdc.NewSDCFHIRPathEngine(fhirpath.Config{})
```

Use with `sdc.ComposeExpressions` for calculated, initial, and constraint fields.

### Mode: Subscriptions and filters

```go
trigger := subscriptions.Trigger{
    ResourceType: "Observation", Event: subscriptions.TriggerEventCreate,
    FilterFHIRPath: "code.coding.code = '8867-4'",
}
matcher := &subscriptions.Matcher{Engine: engine, Registry: searchRegistry}
```

## Examples

**Envelope vs proto roots** — prefer `*types.ResourceEnvelope`; proto path used when `envelope.Proto` is set or when passing `*patient_go_proto.Patient` directly. Raw `[]byte` and `map[string]any` return `ErrInvalidInput`.

**Custom functions** at engine creation:

```go
eng, _ := fhirpath.NewEngine(fhirpath.Config{
    Functions: map[string]fhirpath.Function{ /* see README tests */ },
    FunctionArity: map[string]int{"echoPrefix": 1},
})
```

**Optional `resolve()` and terminology:**

```go
eng, _ := fhirpath.NewEngine(fhirpath.Config{
    Resolve:     fhirpath.ResourceStoreResolver(resources.Read),
    Terminology: fhirpath.TerminologyServiceAdapter(myValidateCode),
})
```

See `pkg/fhirpath/fhirpath_env_test.go` and `engine_test.go` for navigation, aggregates, and error cases.

## Configuration / key types

| Type / field | Role |
|--------------|------|
| `Engine` | `Eval`, `Compile`, `EvalString`, `EvalBool`, `EvalWithEnv` |
| `Config.CacheSize` | Default 256 compiled-expression cache entries |
| `Config.MaxExpressionLen` | Default 4096 |
| `Config.MaxResultItems` | Default 1024 |
| `Config.ProtoCodec` | Default `proto.NewGoogleR4Codec()` for JSON envelopes |
| `Config.Resolve` / `Terminology` | Opt-in backends for advanced functions |
| `Value` | Result item with `Type()`, `String()`, `Bool()`, `Raw()` |

Stable errors: `ErrInvalidInput`, `ErrExpressionTooLong`, `ErrTooManyResults`, `ErrEmptyResult`, `ErrNotSingleton`, `ErrTypeMismatch`, `ErrNotSupported`, `ErrShadowsBuiltin`.

## Where it fits

| Layer | Role |
|-------|------|
| **types** | `ResourceEnvelope` input |
| **proto** | R4 proto codec and envelope proto field |
| **fhirpath** | Expression engine (this package) |
| **search / view / sdc** | Primary consumers |

## Limits

- One resource root per evaluation (no graph queries without `Resolve`)
- Google FHIR R4 proto only for typed roots
- Full normative FHIRPath compliance not guaranteed beyond tested subset
- `resolve()` / `memberOf()` require explicit configuration
- Does not persist or mutate resources

## Related docs

- [pkg/search/README.md](../search/README.md) — indexer and MatchResourceParameter
- [pkg/view/README.md](../view/README.md) — ViewDefinition parser and executor
- [pkg/sdc/README.md](../sdc/README.md) — SDC FHIRPath engine
- [doc.go](./doc.go) — full API boundaries
