# haistack-cql (`pkg/cql`)

Bounded [Clinical Quality Language](https://cql.hl7.org) evaluation for SDC
questionnaires and in-memory Patient-context reasoning.

This package **runs CQL libraries and inline expressions** against a Patient
you already have in memory (and, optionally, retrieved clinical resources).
It does **not** execute CQF Measure reports or clinical quality pipelines.

## CQL vs FHIRPath

| | FHIRPath (`pkg/fhirpath`) | CQL (`pkg/cql`) |
|--|--|--|
| Typical SDC language | `text/fhirpath` | `text/cql`, `text/cql.identifier`, `application/cql`, `application/x-cql` |
| Unit of reuse | One path on the current resource | Named `define` statements in a Library |
| Context | Current resource (`%resource`, `%subject`) | `context Patient` plus parameters |
| Clinical helpers | Custom functions you register | Built-ins such as `AgeInYears()` |
| Retrieve | No (use FHIR Query / search) | `[Observation]` when a Retriever is wired |

Use FHIRPath for field access and enablement on the resource in focus. Use CQL
when the questionnaire references a `cqf-library` (or contained Library) and
expressions should share named clinical logic.

## Supported subset

- `library` / `using FHIR` / `include` / `context Patient`
- `define` named expressions
- literals, identifiers, arithmetic, comparison, `and` / `or` / `not`
- `if then else`, `is null` / `is not null`
- FHIR property navigation (`Patient.name.given`)
- retrieve `[ResourceType]` when `Config.Retriever` is set
- `First`, `Last`, `Count`, `Exists`, `AgeInYears`, `ToString` and related helpers

Unsupported (clear error): `define function`, ELM-only libraries, related-context
retrieve, Interval promotion, terminology membership (`in SomeValueSet`), and
CQF Measure evaluation.

## Usage

```go
eng, err := cql.NewEngine(cql.Config{})
patient := envelope // *types.ResourceEnvelope for Patient

values, err := eng.Eval(ctx, "First(Patient.name.given)", cql.EvalContext{
    Patient: patient,
})

lib, err := eng.ParseLibrary(cqlSource)
values, err = eng.EvalDefine(ctx, lib, "Patient Given Name", cql.EvalContext{
    Patient: patient,
})
```

Load a FHIR Library resource:

```go
src, url, name, version, err := cql.EnvelopeLibrary(libraryEnvelope)
lib, err := compile // eng.ParseLibrary(src)
```

## SDC adapter

`cql.Provider` implements `sdc.CQLProvider`. Wire it with
`sdc.ComposeExpressions` so `$populate`, `$validate`, and `Render` can evaluate
CQL expressions:

```go
eng, _ := cql.NewEngine(cql.Config{FHIRPath: fhirPathEngine})
cqlProvider := cql.NewProvider(eng, &cql.StoreLibraryResolver{
    Resources: resourceStore,
    Engine:    eng,
})
provider := sdc.ComposeExpressions(
    sdc.FHIRPathExpressions{Engine: fhirPathEngine},
    fhirQuery, // optional
    cqlProvider,
)
```

The default `pkg/runtime` wiring does this automatically: CQL libraries are
resolved from stored `Library` resources, questionnaire `contained` Libraries,
and `cqf-library` canonicals. Missing Patient context or a missing library
returns a diagnostic rather than a silent empty answer.

Questionnaire expressions:

- `text/cql` / `application/cql` / `application/x-cql` — inline CQL
- `text/cql.identifier` — name of a `define` in a loaded library

## Errors

- `ErrMissingContext` — Patient context required but not supplied
- `ErrLibraryNotFound` — canonical Library could not be loaded
- `ErrExpressionNotFound` — named define is missing
- `ErrUnsupported` — feature outside the subset (including ELM-only content)
- `ErrEmptyExpression` / `ErrEngineUnavailable`

See [`doc.go`](./doc.go) for package boundaries and the tests in this directory
for executable examples.
