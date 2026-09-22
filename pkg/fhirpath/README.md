# haistack-fhirpath (`pkg/fhirpath`)

In-memory FHIRPath evaluation for a **single FHIR resource at a time**.

## What it does

[FHIRPath](https://build.fhir.org/ig/HL7/FHIRPath/) is a small query language for FHIR — like XPath for XML. You write expressions such as:

- `Patient.name.given` — given names
- `Patient.telecom.where(system = 'phone').value` — phone numbers
- `Observation.value.ofType(Quantity).value` — a numeric observation value

This package **runs those expressions** against a Patient, Observation, or other resource you already have in memory. It does **not** search a database or fetch resources by itself; it only reads fields from a resource you pass in.

**Given this one FHIR resource, what values does this path return?**

The implementation wraps [github.com/verily-src/fhirpath-go](https://github.com/verily-src/fhirpath-go) behind a stable haistack API (`Engine`, `CompiledExpression`, `Value`).

## How it fits in the stack

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
| **pkg/search** | `RegistryIndexer` evaluates each installed SearchParameter expression; `MatchResourceParameter` re-evaluates for subscription-style param matching |
| **pkg/view** | `ParseDefinition` / `Registry.Register` compile filter and column expressions at parse time; `Executor` evaluates them per resource during scans |
| **pkg/sdc** | Questionnaire `text/fhirpath` expressions (calculated, initial, constraint, toggle); `NewSDCFHIRPathEngine` adds `weight()` |
| **pkg/subscriptions** | `Matcher.FilterFHIRPath` predicates on create/update events |
| **pkg/structuremap** | StructureMap engine holds a shared `fhirpath.Engine` for dependent logic |
| **pkg/smart** | Scope filter matchers can chain registry + fhirpath (see `docs/smart-auth-architecture.md`) |
| **cmd/haistack** | `haistack fhirpath eval <file> <expression>` for local debugging |

This package must not import query backends (`store`, `sqlite`, `postgres`).

## Inputs: envelope vs proto

Accepted evaluation roots:

- `*types.ResourceEnvelope` — preferred runtime container from `pkg/core` and stores
- Google FHIR R4 protobuf resources recognized by `pkg/proto.IsProtoResource` (including `*ContainedResource` with a populated branch)

Rejected (returns `ErrInvalidInput`): raw `[]byte`, `map[string]any`, arbitrary Go structs, nil envelopes without JSON/proto, unsupported proto versions.

**Adaptation for envelopes:**

| `envelope.Proto` | Evaluation path |
|------------------|-----------------|
| Non-nil, supported R4 message | Proto is unwrapped and passed to the backend directly |
| Nil | `envelope.JSON` is parsed through `Config.ProtoCodec` (default: `proto.NewGoogleR4Codec()`) |

JSON remains the canonical stored form across haistack; proto is an optional fast path that must represent the same resource as `envelope.JSON`. Tests in `pkg/proto` assert proto and JSON ingestion produce identical `Hash` values on the envelope.

Direct proto input (bypassing envelope) is valid for tools and tests:

```go
import patientpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/resources/patient_go_proto"

values, err := eng.Eval(ctx, "Patient.name.family", &patientpb.Patient{...})
```

## Engine API: Eval vs Compile

```go
eng, err := fhirpath.NewEngine(fhirpath.Config{})
ctx := context.Background()

// One-shot: compile (or cache hit) + evaluate
values, err := eng.Eval(ctx, "Patient.name.given", envelope)

// Strict singleton coercion
name, err := eng.EvalString(ctx, "Patient.name.given.first()", envelope)
exists, err := eng.EvalBool(ctx, "Patient.name.exists()", envelope)

// External constants / %variables (when supported by expression)
values, err := eng.EvalWithEnv(ctx, expr, envelope, map[string]any{"ctx": "value"})
```

Reuse a compiled expression in a hot loop (index rebuild, view scan, batch eval):

```go
compiled, err := eng.Compile("Patient.telecom.where(system = 'phone').value")
if err != nil { /* parse error */ }

for _, env := range patients {
    phones, err := compiled.Eval(ctx, env)
    for _, v := range phones {
        s, _ := v.String()
        _ = s // index or project
    }
}
```

`Compile` is concurrency-safe and caches by expression string (cache size default 256). `CompiledExpression` values are safe to share across goroutines.

## Working with results

`Eval` returns `[]Value` (a collection). An empty collection is `nil`.

| Method | Use when |
|--------|----------|
| `v.Type()` | Stable type name (`String`, `Patient`, `HumanName`, …) |
| `v.String()`, `v.Bool()`, `v.Float64()` | Coerce one item |
| `v.Raw()` | Backend-native value for proto-aware integrations |

`EvalBool` / `EvalString` enforce: exactly one item, correct type — otherwise `ErrEmptyResult`, `ErrNotSingleton`, or `ErrTypeMismatch`.

## Usage in search

Wire the indexer on the core write path:

```go
indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{
    Registry: search.NewSnapshotRegistry(snapshot),
    Engine:   fhirpathEngine,
})
```

For each saved resource, the indexer evaluates registry expressions, then `normalizeValues` maps `fhirpath.Value` collections into typed index keys (`token.*`, `string.*`, `date.*`, `reference.*`, …). Unsupported expressions surface as indexing skips when `ErrNotSupported` is returned.

Runtime param matching (subscriptions criteria helpers):

```go
matched, known := search.MatchResourceParameter(ctx, reg, engine, "Observation", envelope, "code", wantTokens)
```

## Usage in view (SQL-on-FHIR ViewDefinition)

Views require an engine at parse time so invalid FHIRPath fails before scan:

```go
engine, _ := fhirpath.NewEngine(fhirpath.Config{})
parser, _ := view.NewDefinitionParser(engine)
spec, err := parser.Parse(viewDefinitionJSON)

reg := view.NewRegistry()
_, err = reg.Register(spec, engine)

exec, _ := view.NewExecutor(view.Config{
    Resources: resourceStore,
    Engine:    engine,
    Registry:  reg,
})
result, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: spec.Name})
```

Root `where` clauses act as FHIRPath predicates; column and nested `select` trees evaluate paths per matching resource. Reference joins resolve through the resource store (not via bare `resolve()` unless you configure a resolver on the engine).

## Usage in SDC and subscriptions

SDC composes FHIRPath with FHIR Query and CQL via `sdc.ComposeExpressions`. Questionnaire items use `Expression{Language: "text/fhirpath", Expression: "..."}` for calculated, initial, required, and toggle fields.

Production SDC engines should use SDC-aware construction:

```go
engine, err := sdc.NewSDCFHIRPathEngine(fhirpath.Config{})
// registers weight() for answer-option scoring expressions
```

Subscription triggers can combine search criteria with path filters:

```go
trigger := subscriptions.Trigger{
    ResourceType:   "Observation",
    Event:          subscriptions.TriggerEventCreate,
    FilterFHIRPath: "code.coding.code = '8867-4'",
}
matcher := &subscriptions.Matcher{Engine: engine, Registry: searchRegistry}
```

## Custom functions

Register application-specific functions at `NewEngine` time (immutable for the engine lifetime):

```go
import "github.com/verily-src/fhirpath-go/fhirpath/system"

eng, err := fhirpath.NewEngine(fhirpath.Config{
    Functions: map[string]fhirpath.Function{
        "alwaysTrue": func(_ fhirpath.Collection, _ ...fhirpath.Collection) (fhirpath.Collection, error) {
            return fhirpath.Collection{fhirpath.NewValue(system.Boolean(true))}, nil
        },
        "echoPrefix": func(_ fhirpath.Collection, args ...fhirpath.Collection) (fhirpath.Collection, error) {
            s, err := args[0][0].String()
            if err != nil { return nil, err }
            return fhirpath.Collection{fhirpath.NewValue(system.String("prefix-" + s))}, nil
        },
    },
    FunctionArity: map[string]int{"echoPrefix": 1},
})
```

Names that shadow built-in FHIRPath functions return `ErrShadowsBuiltin`. Maximum declared arity is `MaxCustomFunctionArity` (8).

## Optional resolve() and terminology

By default, expressions using `resolve()` or terminology functions may return `ErrNotSupported`. Opt in when you can supply backends:

```go
eng, _ := fhirpath.NewEngine(fhirpath.Config{
    Resolve: fhirpath.ResourceStoreResolver(func(ctx context.Context, rt, id string) (any, error) {
        return resources.Read(ctx, rt, id)
    }),
    Terminology: fhirpath.TerminologyServiceAdapter(myValidateCode),
})
```

Helpers:

- `EnhancedResourceStoreResolver` — absolute REST URLs, `urn:uuid`, untyped ids with optional logical-id resolver
- `WithEvaluationResource` — attaches the root resource to context for contained `#fragment` references
- `ParseReferenceForRead` — shared reference parsing for resolver implementations

When `Resolve` is nil, `resolve()` errors map to unsupported/unconfigured resolver errors. When `Terminology` is set, experimental terminology tables in the Verily backend are enabled for `memberOf()` and related functions.

## Configuration, errors, and limits

Defaults: `CacheSize` 256, `MaxExpressionLen` 4096, `MaxResultItems` 1024, `ProtoCodec` = `proto.NewGoogleR4Codec()`. Evaluation honors `context.Context`; `DefaultTimeout` is a soft deadline when the caller context has none.

Stable errors include `ErrInvalidInput`, `ErrExpressionTooLong`, `ErrTooManyResults`, `ErrEmptyResult`, `ErrNotSingleton`, `ErrTypeMismatch`, `ErrNotSupported`, and `ErrShadowsBuiltin`. Tests cover common navigation, filtering, aggregates, comparisons, and string helpers such as `contains()`; full normative compliance is not guaranteed beyond that subset.

See [doc.go](./doc.go) for the full API and package boundaries.
