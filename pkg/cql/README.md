# haistack-cql (`pkg/cql`)

[Clinical Quality Language](https://cql.hl7.org) 1.5 evaluation for SDC
questionnaires, in-memory clinical reasoning, and CQF Measure reports.

Import path: `github.com/degoke/haistack/pkg/cql`.

---

## What it does

This package **runs CQL libraries and inline expressions** against a Patient
(and optionally retrieved clinical resources). It evaluates FHIR Measure resources
into MeasureReport (`individual`, `summary`, `subject-list`).

| Capability | Entry points |
|------------|--------------|
| Inline / library eval | `Engine.Eval`, `EvalDefine`, `ParseLibrary`, `CompileLibrary` |
| FHIR Library ingest | `EnvelopeLibrary`, `AttachLibraryCQL`, `application/elm+json` |
| SDC expressions | `Provider` implements `sdc.CQLProvider` |
| Quality reporting | `EvaluateMeasure`, `MeasureRequest` |
| Retrieve | `[Observation]` when `Config.Retriever` is wired |
| Terminology | Value-set `MemberOf`, `Subsumes` when `Config.Terminology` is set |

FHIR Library resources may include `text/cql` (or `application/cql`) source or
`application/elm+json`. ELM JSON compiles into the same AST as CQL. Empty ELM payloads
and `application/elm+xml` return `ErrUnsupported`.

The default **`pkg/runtime`** wiring loads libraries from stored `Library` resources,
questionnaire `contained` Libraries, and `cqf-library` canonicals via
`StoreLibraryResolver`.

---

## How it fits in the ecosystem

```text
Questionnaire / Measure (ResourceEnvelope)
        │
        ├── pkg/sdc ($populate, $validate, Render)
        │        └── ComposeExpressions(FHIRPath, FHIR Query, cql.Provider)
        │
        ▼
   cql.Engine
        │
        ├── Config.FHIRPath ──► pkg/fhirpath (navigation)
        ├── Config.Retriever ──► store / search ( [Observation] )
        ├── Config.Terminology ──► pkg/terminology (MemberOf, Subsumes)
        └── Config.UCUM ──► quantity math

HTTP: Measure/$evaluate-measure via http.CoreMeasureService
Examples: modules/sdc/examples/cql-questionnaire.json, cql-library.json
```

| Concern | Package |
|---------|---------|
| Field paths on current resource | `pkg/fhirpath` |
| Named clinical logic in Libraries | `pkg/cql` (this) |
| Questionnaire behavior | `pkg/sdc` |
| StructureMap extraction | `pkg/structuremap` (not CQL) |

---

## CQL vs FHIRPath

| | FHIRPath (`pkg/fhirpath`) | CQL (`pkg/cql`) |
|--|--|--|
| Typical SDC language | `text/fhirpath` | `text/cql`, `text/cql.identifier`, `application/cql`, `application/x-cql` |
| Unit of reuse | One path on the current resource | Named `define` / `define function` in a Library |
| Context | Current resource (`%resource`, `%subject`) | `context Patient` plus parameters |
| Clinical helpers | Custom registered functions | `AgeInYears()`, intervals, quantities |
| Retrieve | No (use FHIR Query / search) | `[Observation]` when Retriever wired |
| Quality reporting | No | `EvaluateMeasure` → MeasureReport |

Use FHIRPath for field access and enablement on the resource in focus. Use CQL when
the questionnaire references a `cqf-library` (or contained Library) and expressions
should share named clinical logic, or when evaluating a Measure.

---

## When to use it

- **SDC populate** — initial values and calculated fields from shared Library defines
- **Enablement / validation** — CQL predicates in `RenderWithOptions` and `ValidationOptions`
- **Measure reporting** — `$evaluate-measure` with period and subject parameters
- **ELM-only libraries** — compile `application/elm+json` when CQL source is not attached

Prefer FHIRPath for simple item paths. Prefer FHIR Query (`application/x-fhir-query`)
for search-shaped context when CQL retrieve is not wired.

---

## Usage modes

### 1. Direct engine evaluation

```go
eng, err := cql.NewEngine(cql.Config{FHIRPath: fhirPathEngine})
patient := envelope // *types.ResourceEnvelope for Patient

values, err := eng.Eval(ctx, "First(Patient.name.given)", cql.EvalContext{
    Patient: patient,
})

lib, err := eng.ParseLibrary(cqlSource)
values, err = eng.EvalDefine(ctx, lib, "Patient Given Name", cql.EvalContext{
    Patient: patient,
})
```

Load from a FHIR Library envelope:

```go
lib, err := eng.CompileLibrary(libraryEnvelope) // text/cql or application/elm+json
src, url, name, version, err := cql.EnvelopeLibrary(libraryEnvelope)
env, err := cql.AttachLibraryCQL(libraryEnvelope, cqlSource)
```

### 2. SDC adapter (`cql.Provider`)

From `sdc_test.go` `TestSDCPopulateUsesCQL`:

```go
eng := testEngine(t)
lib, _ := eng.ParseLibrary(demoLibrarySource())
lib.URL = "http://example.org/Library/PatientLogic"
provider := cql.NewProvider(eng, cql.StaticLibraries{lib.URL: lib})

q := sdc.NewDraft("http://example.org/Questionnaire/cql", []sdc.Item{
    {LinkID: "given", Type: "string",
        InitialExpression: &sdc.Expression{
            Language: sdc.CQLIdentifierLanguage, Expression: "Patient Given Name"}},
    {LinkID: "adult", Type: "boolean",
        InitialExpression: &sdc.Expression{
            Language: sdc.CQLLanguage, Expression: "AgeInYears() >= 18"}},
})
q.CQFLibraries = []sdc.CQFLibraryRef{{LibraryCanonical: lib.URL}}

resp, outcome := sdc.Populate(ctx, q, sdc.PopulationContext{
    Subject:  patientEnvelope,
    Provider: sdc.ComposeExpressions(nil, nil, provider),
})
```

Wire with store-backed libraries:

```go
cqlProvider := cql.NewProvider(eng, &cql.StoreLibraryResolver{
    Resources: resourceStore,
    Engine:    eng,
})
provider := sdc.ComposeExpressions(
    sdc.FHIRPathExpressions{Engine: fhirPathEngine},
    fhirQuery, // optional SearchFHIRQueryProvider
    cqlProvider,
)
```

**Validate and render with CQL** (`TestSDCValidateAndRenderUseCQL`): enablement
expressions such as `Patient.active` gate required items; inactive patients skip
required validation when the item is disabled.

**Missing library / patient** (`TestSDCPopulateMissingLibrary`, `TestSDCPopulateMissingPatient`):
returns OperationOutcome-style diagnostics, not silent empty answers.

**Contained library** (`TestSDCPopulateContainedLibrary`): resolves Library from
questionnaire `contained`.

### 3. Measure evaluation

From `measure_test.go`:

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

Tests cover individual/summary reports, stratifiers, SDE, IP gates, list membership,
and closed period end handling (`TestEvaluateMeasureIndividualAndSummary`,
`TestEvaluateMeasureListMembershipAndStratifierAndSDE`, and related cases).

HTTP: GET/POST `/fhir/Measure/{id}/$evaluate-measure` with `periodStart`, `periodEnd`,
`reportType`, `subject` query or Parameters body.

### 4. ELM JSON libraries

`TestELMFunctionReturnType`, `TestELMQueryLetRef`, `TestELMAndCQLDateFrom` in
`elm_test.go` / `elm_nits_test.go` show ELM `FunctionDef` return types and query let refs.

Prefer `AttachLibraryCQL` when original CQL source is available alongside ELM.

### 5. Terminology-aware CQL

`subsumes_terminology_test.go`:

- `TestSubsumesStrictWhenTerminologyWired`
- `TestSubsumesUsesTerminologyValidator`

Wire `Config.Terminology` for value-set membership in retrieve/`in` filters and
`Subsumes` semantics on CodeableConcept/Coding.

---

## CQL 1.5 coverage

- `library` / `using FHIR` / `include` / `context Patient` or `Unfiltered`
- `define` named expressions and `define function` / `define fluent function`
- `parameter` declarations with optional defaults (`Measurement Period` for measures)
- Literals, identifiers, arithmetic, comparison, boolean logic
- `if then else`, `case when else`, `is null` / `is not null`
- FHIR property navigation; uses `Config.FHIRPath` when set
- Retrieve `[ResourceType]` and related-context aliases
- Queries: `from` / `where` / `return` / `sort by` / `with` / `without` / `let`
- **Bracket-query shorthand** (top-level `define` and `Engine.Eval` only):
  `[Observation] O where O.status = 'final'` ≡ `from [Observation] O where ...`
- `define function … returns List<T>` for list-valued results
- `as` promotions between numeric and temporal types; UCUM quantity conversion via `Config.UCUM`
- `Interval` operators; list equivalence (`~`) for `in` / `distinct` / etc.
- `codesystem` / `valueset` / `code` declarations

Unsupported: empty ELM, ELM XML, unimplemented operators (clear `ErrUnsupported`).

---

## Tests as examples

| Topic | Test |
|-------|------|
| Bracket-query shorthand | `TestDefineBracketQueryShorthand` (`engine_test.go`) |
| `returns List<T>` functions | `TestFunctionReturnsListType` (`engine_test.go`) |
| List parameter semantics | `TestListParameterIdentSemantics`, `TestDefineListIdentSemantics` |
| UCUM quantity conversion | `TestConvertQuantityUsesUCUM` (`ucum_test.go`) |
| SDC populate/validate/render | `sdc_test.go` |
| Measure reports | `measure_test.go` |

Demo library source used in tests (`sdc_test.go`):

```cql
library PatientLogic version '1.0.0'
using FHIR version '4.0.1'
context Patient
define "Patient Given Name": First(Patient.name.given)
define "Is Adult": AgeInYears() >= 18
```

---

## Questionnaire expression languages

- `text/cql` / `application/cql` / `application/x-cql` — inline CQL
- `text/cql.identifier` — name of a `define` in a loaded library

Runtime resolves libraries from store, contained resources, and canonical URLs.

---

## Errors

| Error | When |
|-------|------|
| `ErrMissingContext` | Patient context required but not supplied |
| `ErrLibraryNotFound` | Canonical Library could not be loaded |
| `ErrExpressionNotFound` | Named define is missing |
| `ErrUnsupported` | Empty/non-JSON ELM, ELM XML, unimplemented operator |
| `ErrMeasure` | Measure evaluation input or criteria failed |
| `ErrEmptyExpression` / `ErrEngineUnavailable` | Invalid eval request |

---

## Limits

- CQF Measure evaluation covers packaged v1 scenarios; exotic measure logic may hit
  `ErrUnsupported` or partial population groups (see measure tests for supported shapes).
- Retrieve requires an injected Retriever; default edge SQLite may not wire full clinical
  data unless resources are preloaded.
- CQL is not used for SDC StructureMap `$extract` (see `pkg/structuremap`).

---

## Related docs

- [pkg/sdc/README.md](../sdc/README.md) — questionnaire populate/validate/render
- [pkg/fhirpath/README.md](../fhirpath/README.md) — path evaluation
- [pkg/terminology/README.md](../terminology/README.md) — value sets for MemberOf
- [doc.go](./doc.go) — package boundaries
