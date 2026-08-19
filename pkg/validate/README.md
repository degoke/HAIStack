# haistack-validate (`pkg/validate`)

Built-in FHIR resource validation for health-ai-stack.

## What it does

`pkg/validate` checks FHIR resources **before** they are saved, synced, indexed, or handed to other tools. Think of it as a gatekeeper: *Is this resource well-formed enough to work with safely?*

It looks at a `*types.ResourceEnvelope` — mainly the JSON inside it — and checks things like:

- Is the JSON valid?
- Does it have a `resourceType` (e.g. `Patient`, `Observation`)?
- Is that type a known FHIR type?
- Is the `id` valid (if present)?
- Are basic required fields present (e.g. `Observation.status`)?
- Are references syntactically valid (e.g. `Patient/123`, URLs, URNs)?
- Are fields structurally sane (via Google’s FHIR JSON/proto parser — e.g. `active` must be a boolean, not a string)?

**What it does not do (MVP):** full FHIR conformance — profiles, terminology, business rules, or whether a referenced resource actually exists.

## When to use it

- Before persisting a resource through `pkg/core`
- In a CLI `validate` command or import pipeline
- As a guard before exposing data to AI tools or sync

## Usage

### Standalone (CLI, import, AI guard, etc.)

Returns a structured result with a list of issues — good when you want detailed feedback or a FHIR `OperationOutcome`.

```go
import (
    "context"

    "github.com/degoke/health-ai-stack/pkg/validate"
)

eng, err := validate.NewEngine(validate.Config{})
if err != nil {
    // invalid engine config
}

result, err := eng.Validate(ctx, envelope, validate.ValidateOptions{
    RequireID: true, // optional: fail if id is missing
})
if err != nil {
    // exceptional failure (e.g. context cancelled)
}

if !result.Valid {
    for _, issue := range result.Issues {
        // issue.Code, issue.Diagnostics, issue.Expression
    }

    outcome := validate.ToOperationOutcome(result)
    // emit outcome as an API error response
}
```

### Plugged into `pkg/core` (automatic validation on writes)

Core supports an optional validator hook. Wrap the engine so invalid resources are rejected on Create/Update:

```go
import (
    "github.com/degoke/health-ai-stack/pkg/core"
    "github.com/degoke/health-ai-stack/pkg/validate"
)

eng, _ := validate.NewEngine(validate.Config{})

svc, _ := core.NewResourceService(core.ResourceServiceConfig{
    Resources: db,
    History:   db,
    Sessions:  db,
    Validator: validate.NewCoreValidator(eng, validate.ValidateOptions{}),
})
```

If validation fails, core aborts the write and returns `ErrorKindInvalid`. When the failure comes from `NewCoreValidator`, `core.OperationOutcomeFromError` preserves each validation issue code (for example `invalid-id`, `missing-required-field`) instead of collapsing them into a single generic `invalid` issue.

If you omit `Validator`, core skips validation entirely.

## Optional configuration

Restrict which resource types are allowed in a deployment, or add required-field rules:

```go
eng, _ := validate.NewEngine(validate.Config{
    InstalledTypes: validate.MapResourceTypeRegistry{
        "Patient":     {},
        "Observation": {},
    },
    RequiredFields: map[string][]string{
        "Patient": {"gender"},
    },
})
```

You can also pass a per-request allowlist via `ValidateOptions.ResourceTypeRegistry`.

## Where it fits

```
FHIR JSON envelope
       ↓
  validate engine
       ↓
  Valid?  → yes → proceed (save, sync, index, etc.)
       ↓
       no → list of issues (+ optional OperationOutcome)
```

| Layer | Role |
|-------|------|
| **types** | Canonical JSON envelope |
| **validate** | Structural and safety checks before use |
| **core** | Optional validator hook on writes |
| **proto** | Google R4 parsing used for structural/primitive checks |

## MVP limits

- Structural and safety-oriented, not full profile conformance
- Syntactic reference checks only (no existence resolution; bare IDs without slashes are accepted; typed references are not checked against the installed resource-type registry)
- Default required fields: `Observation.status`, `Bundle.type` only — `Patient` has none unless you configure `RequiredFields` (custom maps replace per-type defaults entirely)
- Optional installed resource-type allowlist
- Google FHIR R4 proto/jsonformat for primitive and structural validation; structural diagnostics use FHIR element paths (for example `Patient.id: …`) rather than raw jsonformat prefixes
- No terminology, slicing, custom invariants, or module-specific rules

When `envelope.Proto` is populated, matches the JSON resource type, and `envelope.Hash` still matches canonical JSON, structural validation can reuse the attached proto instead of re-parsing.

See [doc.go](./doc.go) for the full API and package boundaries.
