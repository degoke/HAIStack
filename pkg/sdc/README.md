# haistack-sdc (`pkg/sdc`)

FHIR R4 Structured Data Capture (SDC 3.0.0) behavior services for
questionnaire-driven workflows.

Import path: `github.com/degoke/haistack/pkg/sdc`.

`pkg/sdc` is deliberately **renderer- and transport-neutral**. It does not own HTTP
handlers, UI widgets, database tables, or a second FHIR persistence model.

---

## What it does

| Area | Primary APIs | Output |
|------|--------------|--------|
| Validation | `ValidateQuestionnaireResource`, `ValidateQuestionnaireResponseResource` | OperationOutcome-compatible diagnostics |
| Populate | `PopulateResource`, `PopulationContext` | QuestionnaireResponse envelope (unsaved) |
| Build responses | `NewResponse`, `ResponseBuilder` | QuestionnaireResponse from form values |
| Render | `Render`, `RenderWithOptions` | Renderer-neutral `FormModel` / `FieldState` |
| Calculated | `EvaluateCalculated` | Converged calculated answers |
| Assembly | `Assembler`, `AssembleQuestionnaireResource` | Merged Questionnaire |
| Extract | `ExtractResource`, `QuestionnaireExtractor` | Transaction Bundle envelope |

Canonical interchange is `*types.ResourceEnvelope`, consistent with `pkg/core`,
`pkg/store`, `pkg/http`, and `pkg/runtime`. Generated R4 protobuf types remain in
`pkg/proto/r4`; use `sdc.ParseR4` when typed proto access is required.

Compatibility projections encode polymorphic FHIR values with correct `value[x]` /
`answer[x]` keys and SDC behavior as FHIR extensions. They are **views for behavior
evaluation**, not persistence replacements.

---

## How it fits in the ecosystem

```text
Questionnaire / QuestionnaireResponse (ResourceEnvelope)
        │
        ▼
     pkg/sdc
        │
        ├── pkg/fhirpath ──► initial, enableWhen, calculated, constraints
        ├── pkg/cql ──► cqf-library, text/cql, text/cql.identifier
        ├── pkg/search ──► application/x-fhir-query (optional)
        ├── pkg/terminology ──► answerValueSet, open-choice expansion
        ├── pkg/structuremap ──► sourceStructureMap $extract
        └── pkg/conceptmap ──► (via structuremap translate)

pkg/http ──► Questionnaire/$populate, QR/$validate, QR/$extract, …
pkg/runtime ──► CoreSDCService (default wiring)
modules/sdc ──► profiles, extensions, operation definitions, CQL examples
```

Extraction produces a **transaction Bundle**; applying it is an explicit caller decision
via `core.ProcessTransactionBundle` — no silent persistence inside `pkg/sdc`.

---

## When to use it

- **Server-side questionnaire logic** without coupling to a specific UI framework
- **Populate** empty responses from Patient context and expressions
- **Validate** responses before save with SDC + generic FHIR item constraints
- **Extract** clinical resources from responses (definition, template, or StructureMap)
- **Adaptive flows** via `SequentialAdaptiveEngine` or injected session policy (HTTP still
  needs an application session adapter)

Inject application-owned concerns: phone/email/postal formats, extraction templates,
branching/scoring session policy, and custom adaptive HTTP adapters.

---

## Boundaries

```go
q, err := svc.Read(ctx, "Questionnaire", "intake")
outcome := sdc.ValidateQuestionnaireResource(ctx, q, sdc.ValidationOptions{})
if len(outcome.Issue) != 0 {
    // render or return OperationOutcome-compatible diagnostics
}
```

Storage, auth, and transport live outside this package. Tier-5 extraction metadata
(`sourceStructureMap`, item definitions, `observationExtract`) drives
`QuestionnaireExtractor`; HTTP `$populate` and `$validate` parse Parameters for
`subject` and launch context.

---

## Usage modes

### 1. Validation

`ValidateQuestionnaireResource` checks structure, duplicate linkIds, item types,
enablement declarations, generic constraints (`maxLength`, `regex`,
`questionnaire-constraint`), and required SDC fields.

`ValidateQuestionnaireResponseResource` checks identity, required/disabled items,
repeats/cardinality, answer types/options, terminology, value constraints,
questionnaire-constraint invariants, and calculated/enablement constraints.

SDC presentation extensions (`questionnaire-itemControl`, `questionnaire-entryFormat`,
choice orientation, option exclusive, usage mode, display category, support links,
reference/unit extensions, launch context/variables) surface on `FieldState` for renderers.

`answerValueSet` validation uses the configured terminology service (including expansion
for `open-choice` string answers). Reference answers honor `referenceProfile` and
`referenceFilter` when `ReferenceResolver` is supplied. `usageMode` ties to response
status (capture for in-progress, display for completed/amended).

Tests: `gaps_test.go` (`TestReferenceProfileAndFilterValidation`, `TestLaunchContextValidation`,
`TestUsageModeSemantics`, `TestVariableContextInValidation`, …).

### 2. Response builder

```go
builder, err := sdc.NewResponse(questionnaire)
response, err := builder.
    Set("name", "Ada").
    SetCoding("color", "red").
    InGroup("group", 1).
    Set("nested", true).
    SetAtAnswer(sdc.ItemPath{{LinkID: "trigger"}}, 0, "detail", "extra").
    AppendAnswer("tags", "alpha").
    Build(sdc.ValidationOptions{})
if err != nil {
    if outcome, ok := sdc.OutcomeFromError(err); ok {
        // render outcome.Issue
    }
}
```

Use `SetAt` / `AppendAnswerAt` for duplicate linkIds, `InGroup` for repeating groups,
`SetAtAnswer` for item-controlled nesting. `SetCodingWithSystem` disambiguates codes
across systems. Pre-formed FHIR answer values are stored as-is; validation still
enforces answer options.

### 3. Population (FHIRPath + CQL + FHIR Query)

`PopulateResource` returns a new QuestionnaireResponse without saving. Supports subject,
launch context, initial values/expressions, answer expressions, and injectable providers.

```go
engine, _ := fhirpath.NewEngine(fhirpath.Config{})
response, outcome := sdc.PopulateResource(ctx, questionnaire,
    sdc.PopulationContext{
        Subject:  patientEnvelope,
        Provider: sdc.FHIRPathExpressions{Engine: engine},
    },
)

cqlEngine, _ := cql.NewEngine(cql.Config{FHIRPath: engine})
response, outcome = sdc.PopulateResource(ctx, questionnaire,
    sdc.PopulationContext{
        Subject: patientEnvelope,
        Provider: sdc.ComposeExpressions(
            sdc.FHIRPathExpressions{Engine: engine},
            nil,
            cql.NewProvider(cqlEngine, nil),
        ),
    },
)
```

CQL languages: `text/cql`, `text/cql.identifier`, `application/cql`, `application/x-cql`.
Libraries resolve from `cqf-library` canonicals, contained `Library` resources, or
`cql.LibraryResolver`. Missing Patient or library → unavailable-expression diagnostic.

`SearchFHIRQueryProvider` runs `application/x-fhir-query` against `pkg/search`.
`ComposeExpressions` merges FHIRPath, FHIR Query, and CQL for populate, validate, and
render. `RenderWithOptions` evaluates `contextExpression` extensions into
`FieldState.ContextResources`. FHIR Query substitution: `%subject`, `%patient`, launch
context, questionnaire variables, `%qitem`. Default runtime wires FHIR Query only when
search is enabled.

Executable CQL examples: `pkg/cql/sdc_test.go`, `modules/sdc/examples/cql-questionnaire.json`.

### 4. Calculated expressions and rendering

`EvaluateCalculated` iterates calculated expressions with convergence limits, dependency
inspection, and cycle diagnostics.

`Render` / `RenderWithOptions` produce `FormModel`: visibility, enabled/read-only state,
answers, options, media, item-control metadata, issues, navigation hints. Expression-based
enablement and validation issues can be evaluated during rendering.

Tests: `TestCandidateExpressionOnRender`, `TestAnswerOptionToggleExpression`,
`TestItemPopulationContextScopesDescendants`.

### 5. Modular assembly

`Assembler` resolves questionnaire references via caller-supplied `QuestionnaireResolver`
(no network I/O inside the assembler). `StoreQuestionnaireResolver` uses
`store.ResourceStore` and canonical JSON.

`AssembleQuestionnaireResource` is the envelope-first entry point.

### 6. Extraction

```go
bundle, diagnostics, err := sdc.ExtractResource(ctx, questionnaire,
    questionnaireResponse, extractor)
if err != nil {
    // no resource was persisted or applied
}
result, err := coreService.ProcessTransactionBundle(ctx, bundle)
```

Definition/template extractors support deterministic mappings, repeated answers,
resource identities, POST vs PUT generation, and extraction diagnostics. StructureMap
execution is provided by `pkg/structuremap` when `sourceStructureMap` is set (see
[pkg/structuremap/README.md](../structuremap/README.md)).

Tests: `TestDefinitionExtractionFromItemDefinition`, `TestTemplateExtractUsesContainedResource`,
`TestQuestionnaireExtractorUsesMetadata`.

### 7. HTTP and runtime adapter

`pkg/http` exposes operations when `Config.SDCService` is set. Default runtime wires
`http.CoreSDCService` with core resource service, store-backed questionnaire resolution,
FHIRPath engine, CQL provider, StructureMap extractor, and optional search FHIR Query.

```text
POST /fhir/Questionnaire/{id}/$populate
POST /fhir/Questionnaire/$populate?questionnaire={canonical}
POST /fhir/QuestionnaireResponse/$validate
POST /fhir/QuestionnaireResponse/{id}/$validate
POST /fhir/Questionnaire/{id}/$assemble
POST /fhir/QuestionnaireResponse/$extract
POST /fhir/Questionnaire/$next-question
POST /fhir/Questionnaire/$next
POST /fhir/Questionnaire/$answer
```

Replace or extend the adapter:

```go
rt, err := runtime.New().
    WithSQLite("health.db").
    WithSDC(mySDCService).
    WithHTTP(":8080").
    Build(ctx)
```

Populate, validate, and assemble work with the default adapter. Extraction requires
application mappings/templates unless StructureMap or definition extractors are configured.
Adaptive HTTP requires an application session adapter; package-level callers may use
`SequentialAdaptiveEngine` for deterministic questionnaire-order flow.

Unavailable capabilities return FHIR OperationOutcome errors (not empty success).

---

## Module bundle

`modules/sdc` installs SDC-specific profiles, extensions, operation/capability definitions,
terminology artifacts, and examples. Base R4 types (`Questionnaire`, `QuestionnaireResponse`,
`Bundle`) come from the embedded registry bundle — the SDC module does not duplicate them.

Install via `pkg/modules` / runtime `WithModules`.

---

## Scope summary

**Included:**

- FHIR R4 / SDC 3.0.0 questionnaire behavior
- FHIRPath via `pkg/fhirpath`; CQL via `pkg/cql` (CQL 1.5; Measure in `pkg/cql`)
- Population, validation, assembly, rendering, extraction contracts
- Canonical transaction Bundle generation without persistence side effects
- Adaptive protocol interfaces

**Injected by applications:**

- FHIR Query runtime (default when search enabled)
- Terminology service and value-set expansion
- StructureMap runtime (default when `sourceStructureMap` set)
- Extraction mappings/templates beyond bundled definition extractors
- Adaptive selection and HTTP session policy

---

## Related docs

- [pkg/cql/README.md](../cql/README.md) — CQL provider and libraries
- [pkg/structuremap/README.md](../structuremap/README.md) — StructureMap $extract
- [pkg/fhirpath/README.md](../fhirpath/README.md) — expression evaluation
- [pkg/http/README.md](../http/README.md) — operation routes
- [doc.go](./doc.go) — package-level API boundary
