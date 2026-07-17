# haistack-sdc (`pkg/sdc`)

FHIR R4 Structured Data Capture (SDC 3.0.0) behavior services for
questionnaire-driven workflows.

`pkg/sdc` is deliberately renderer- and transport-neutral. It does not own
HTTP handlers, UI widgets, database tables, or a second FHIR persistence
model.

## Boundaries

Canonical FHIR resources are represented by `*types.ResourceEnvelope`, just as
they are in `pkg/core`, `pkg/store`, `pkg/http`, and `pkg/runtime`.

```go
q, err := svc.Read(ctx, "Questionnaire", "intake")
if err != nil {
    // handle core.ResourceService error
}

outcome := sdc.ValidateQuestionnaireResource(ctx, q, sdc.ValidationOptions{})
if len(outcome.Issue) != 0 {
    // render or return the OperationOutcome-compatible diagnostics
}
```

The generated R4 protobuf types remain available through `pkg/proto/r4`. Use
`sdc.ParseR4` or the existing `pkg/proto` codec when typed protobuf access is
needed; canonical JSON and `ResourceEnvelope` remain the interchange and
storage boundary.

The small questionnaire projection structs retained in the package are
compatibility views for behavior evaluation. They are not replacements for
`pkg/proto/r4` resources and should not be persisted directly.

## Core operations

### Validation

`ValidateQuestionnaireResource` checks questionnaire structure, duplicate
linkIds, item types, enablement declarations, and required SDC fields.
`ValidateQuestionnaireResponseResource` checks response identity, required and
disabled items, repeats/cardinality, answer types, answer options, terminology,
and calculated/enablement constraints.

Diagnostics are OperationOutcome-compatible and retain renderer field paths.

### Population

`PopulateResource` returns a new QuestionnaireResponse envelope without saving
it. Population accepts subject and launch context through `PopulationContext`,
supports initial values/expressions, answer expressions, and injectable
population providers.

FHIRPath is the built-in expression path:

```go
engine, _ := fhirpath.NewEngine(fhirpath.Config{})
response, outcome := sdc.PopulateResource(ctx, questionnaire,
    sdc.PopulationContext{
        Subject:  patientJSON,
        Provider: sdc.FHIRPathExpressions{Engine: engine},
    },
)
```

CQL and FHIR Query are represented by safe provider interfaces. If no provider
is installed, the operation reports an unavailable-expression diagnostic.

### Calculated expressions and rendering

`EvaluateCalculated` iterates calculated expressions with a convergence limit,
dependency inspection, and explicit cycle diagnostics. `Render` produces a
renderer-neutral `FormModel` containing field visibility, enabled/read-only
state, answers, options, media, issues, and navigation hints.

### Modular assembly

`Assembler` resolves questionnaire references through a caller-supplied
`QuestionnaireResolver`. It does not perform network access itself. A
store-backed resolver, `StoreQuestionnaireResolver`, uses the existing
`store.ResourceStore` and canonical JSON resources.

### Extraction

Definition, template, and StructureMap extractor contracts produce a canonical
transaction Bundle envelope:

```go
bundle, diagnostics, err := sdc.ExtractResource(ctx, questionnaire,
    questionnaireResponse, extractor)
if err != nil {
    // no resource was persisted or applied
}

// Applying it is an explicit caller decision:
result, err := coreService.ProcessTransactionBundle(ctx, bundle)
```

Definition extraction supports deterministic mappings, repeated answers,
resource identities, POST-versus-PUT request generation, and extraction
diagnostics. StructureMap execution is an adapter contract; no StructureMap
runtime is bundled.

## HTTP and runtime

`pkg/http` provides the transport adapter when `Config.SDCService` is set. The
runtime wires `http.CoreSDCService` by default using the existing core resource
service, store-backed questionnaire resolution, and runtime FHIRPath engine.

Supported operation routes include:

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

Populate, validate, and assemble work with the default adapter. Extraction
requires application mappings/templates; adaptive routes require an injected
session policy. Unavailable capabilities return FHIR OperationOutcome errors.

Applications can replace or extend the adapter through the runtime builder:

```go
rt, err := runtime.New().
    WithSQLite("health.db").
    WithSDC(mySDCService).
    WithHTTP(":8080").
    Build(ctx)
```

## Module bundle

`modules/sdc` is an installable module containing SDC-specific profiles,
extensions, operation/capability definitions, terminology artifacts, and
examples. Base FHIR R4 definitions such as `Questionnaire`,
`QuestionnaireResponse`, and `Bundle` come from the embedded registry bundle;
the SDC module does not duplicate them.

Install it through the normal module manager or runtime `WithModules` path.

## Scope and adapters

Included:

- FHIR R4 / SDC 3.0.0 questionnaire behavior
- FHIRPath expression integration
- population, validation, assembly, rendering state, and extraction contracts
- canonical transaction Bundle generation without persistence side effects
- adaptive protocol interfaces

Injected by applications:

- CQL runtime
- FHIR Query runtime
- terminology service and value-set expansion
- StructureMap runtime
- extraction mappings/templates
- adaptive questionnaire selection and session policy

See [`doc.go`](./doc.go) for the package-level API boundary and the tests in
this directory for executable behavior examples.
