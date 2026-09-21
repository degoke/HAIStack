# haistack-cql (`pkg/cql`)

[Clinical Quality Language](https://cql.hl7.org) 1.5 evaluation for SDC
questionnaires, in-memory clinical reasoning, and CQF Measure reports.

This package **runs CQL libraries and inline expressions** against a Patient
(and, optionally, retrieved clinical resources). It also evaluates FHIR
Measure resources into MeasureReport (`individual`, `summary`, `subject-list`).
FHIR Library resources may include `text/cql` (or `application/cql`) source
or `application/elm+json`. ELM JSON is compiled into the same evaluator used
for CQL. Empty ELM payloads and `application/elm+xml` return `ErrUnsupported`.

## CQL vs FHIRPath

| | FHIRPath (`pkg/fhirpath`) | CQL (`pkg/cql`) |
|--|--|--|
| Typical SDC language | `text/fhirpath` | `text/cql`, `text/cql.identifier`, `application/cql`, `application/x-cql` |
| Unit of reuse | One path on the current resource | Named `define` / `define function` in a Library |
| Context | Current resource (`%resource`, `%subject`) | `context Patient` plus parameters |
| Clinical helpers | Custom functions you register | Built-ins such as `AgeInYears()` |
| Retrieve | No (use FHIR Query / search) | `[Observation]` when a Retriever is wired |
| Quality reporting | No | `EvaluateMeasure` → MeasureReport |

Use FHIRPath for field access and enablement on the resource in focus. Use CQL
when the questionnaire references a `cqf-library` (or contained Library) and
expressions should share named clinical logic, or when evaluating a Measure.

## CQL 1.5 coverage

- `library` / `using FHIR` / `include` / `context Patient` or `Unfiltered`
- `define` named expressions and `define function` / `define fluent function`
- `parameter` declarations with optional defaults (`Measurement Period` for measures)
- literals, identifiers, arithmetic, comparison, `and` / `or` / `xor` / `not` / `implies`
- `if then else`, `case when else`, `is null` / `is not null`
- FHIR property navigation (`Patient.name.given`); uses `Config.FHIRPath` when set, otherwise JSON navigation
- retrieve `[ResourceType]` and related-context aliases (`[Observation] O`)
- queries: `from` / `where` / `return` / `sort by` / `with` / `without` / `let`
- `Interval` values and operators (`in`, `contains`, `during`, `includes`, `overlaps`, …)
- Quantity literals (`5 'mg'`, `1 year`) and `duration in years between`
- `codesystem` / `valueset` / `code` declarations
- retrieve and `in` filters against FHIR `Coding` / `CodeableConcept` (value-set `MemberOf` when `Config.Terminology` is set)
- `First`, `Last`, `Count`, `Exists`, `AgeInYears`, `ToString`, `ToInterval`, `Min` / `Max` / `Sum` and related helpers

Unsupported (clear error): empty ELM payloads, `application/elm+xml`, or an
unimplemented operator. `application/elm+json` Libraries compile into this
package's AST. Prefer attaching `text/cql` with `AttachLibraryCQL` when the
original source is available.

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

Evaluate a Measure:

```go
report, err := eng.EvaluateMeasure(ctx, cql.MeasureRequest{
    Measure:     measureEnvelope,
    PeriodStart: start,
    PeriodEnd:   end,
    ReportType:  "individual",
    Patient:     patient,
    Libraries:   []*cql.Library{lib},
})
```

Load a FHIR Library resource:

```go
lib, err := eng.CompileLibrary(libraryEnvelope) // text/cql or application/elm+json
src, url, name, version, err := cql.EnvelopeLibrary(libraryEnvelope) // CQL source only
env, err = cql.AttachLibraryCQL(libraryEnvelope, cqlSource)
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

## Measure/$evaluate-measure

Runtime wires `http.CoreMeasureService` so GET/POST
`/fhir/Measure/{id}/$evaluate-measure` (and type-level
`/fhir/Measure/$evaluate-measure?measure=`) return a MeasureReport.

Query or Parameters inputs: `periodStart`, `periodEnd` (required), `reportType`
(`individual` / `subject-list` / `summary`), `subject` (`Patient/{id}`).

## Errors

- `ErrMissingContext` — Patient context required but not supplied
- `ErrLibraryNotFound` — canonical Library could not be loaded
- `ErrExpressionNotFound` — named define is missing
- `ErrUnsupported` — empty/non-JSON ELM, ELM XML, or an unimplemented operator
- `ErrMeasure` — Measure evaluation input or criteria failed
- `ErrEmptyExpression` / `ErrEngineUnavailable`

See [`doc.go`](./doc.go) for package boundaries and the tests in this directory
for executable examples.
