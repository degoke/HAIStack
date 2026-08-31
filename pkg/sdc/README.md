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

The compatibility projections encode polymorphic FHIR values with their
correct `value[x]`/`answer[x]` keys and encode SDC behavior fields as FHIR
extensions. Projection envelopes are therefore hash-stable across a
decode/encode round trip.

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

### Response builder

`NewResponse` constructs a `QuestionnaireResponse` from rendered form values.
The builder sets `QuestionnaireResponse.questionnaire` to `Canonical(q)`,
emits the correct FHIR `value[x]` field for each item type, resolves choice
codes to declared `answerOption` codings, and preserves nested and repeated
items. Unknown linkIds and invalid values are reported as structured issues
through `ValidationError`.

```go
builder, err := sdc.NewResponse(questionnaire)
if err != nil {
    // invalid questionnaire structure
}

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

Use `SetAt` / `AppendAnswerAt` when a linkId appears at multiple nesting
levels, `InGroup` to target a repeating-group instance during auto-placement,
and `SetAtAnswer` / `AppendAnswerAtAnswer` for item-controlled nesting under
`answer[n].item`. Use `SetCodingWithSystem` when answer options share the same
code in different code systems. Already-formed FHIR answer values (for example
a `Coding` struct) are stored without modification; SDC validation still
enforces answer options.

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
        Subject:  patientEnvelope,
        Provider: sdc.FHIRPathExpressions{Engine: engine},
    },
)
```

The built-in provider adapts Questionnaire and QuestionnaireResponse
projections and resource-shaped JSON maps. A subject should normally be a
`*types.ResourceEnvelope` or supported R4 proto resource.

CQL and FHIR Query are represented by safe provider interfaces. If no provider
is installed, the operation reports an unavailable-expression diagnostic.

### Calculated expressions and rendering

`EvaluateCalculated` iterates calculated expressions with a convergence limit,
dependency inspection, and explicit cycle diagnostics. `Render` produces a
renderer-neutral `FormModel` containing field visibility, enabled/read-only
state, answers, options, media, item-control metadata, issues, and navigation
hints. Use `RenderWithOptions` when expression-based enablement or validation
issues should be evaluated during rendering.

### Modular assembly

`Assembler` resolves questionnaire references through a caller-supplied
`QuestionnaireResolver`. It does not perform network access itself. A
store-backed resolver, `StoreQuestionnaireResolver`, uses the existing
`store.ResourceStore` and canonical JSON resources.

Envelope-first callers can use `AssembleQuestionnaireResource` to perform the
same operation without manually decoding the projection.

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
requires application mappings/templates. Package-level adaptive callers can
use the bundled `SequentialAdaptiveEngine` for deterministic
questionnaire-order flow or inject a session policy for branching/scoring
behavior. The HTTP adapter still requires an application session adapter.
Unavailable capabilities return FHIR OperationOutcome errors.

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
