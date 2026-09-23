# haistack-ai (`pkg/ai`)

Policy-governed FHIR AI gateway for HAIStack. LLMs call typed,
audited tools — not arbitrary FHIR server commands.

## What it does

v1 centers on four generic tools that sit in front of the existing stack:

- `read_fhir_resource` — read one resource through policy allow-lists
- `search_fhir_resources` — search with allow-listed parameters and bounded paging
- `run_view` — execute a registered ViewDefinition for structured context
- `write_fhir_resource` — structured create/update with validation and optional approval

Convenience wrappers (`get_patient_summary`, `get_upcoming_appointments`,
`search_patient_by_phone`) delegate to these generic operations and are
pre-registered in `Registry`.

In short: **given policy rules and typed tool input, produce safe structured
context with citations and audit records.**

## What it does not do

- Expose raw FHIR REST or arbitrary SQL to models
- Own OAuth, conversation storage, or prompt templates (host app responsibility)
- Replace `pkg/auth` — use `AIPolicyAdapter` or `AllowListPolicy` for decisions
- Guarantee model output safety beyond tool boundaries and policy

## How it fits in the ecosystem

```text
  LLM / agent orchestrator
            |
            v
     ai.Executor.ExecuteTool / InvokeModel
            |
    +-------+-------+-------+-------+
    |       |       |       |       |
    v       v       v       v       v
 Policy  core   search  view   validate
    |                       |
    v                       v
 pkg/auth              pkg/fhirpath (via view)
 (AIPolicyAdapter)          |
                            v
                      structured rows + citations
                            |
                            v
                      pkg/audit (AuditStoreAdapter)
```

| Direction | Package | Relationship |
|-----------|---------|--------------|
| Upstream | **core** | Validated writes for `write_fhir_resource` |
| Upstream | **search** | Parameterized lookup for `search_fhir_resources` |
| Upstream | **view** | `run_view` executes registered ViewDefinitions |
| Upstream | **validate** | Structural checks on write field maps |
| Peer | **auth** | `AIPolicyAdapter` implements `PolicyEngine` |
| Peer | **audit** | Tool/model events via `AuditStoreAdapter` |
| Downstream | **testkit/aitest** | Harness for executor tests (test-only) |

## When to use it

- Building an agent that needs FHIR context with enforceable allow-lists
- Exposing tool descriptors to models (`Registry.AllToolDescriptors`)
- Requiring human approval before sensitive writes
- De-identifying reads/search/view rows when policy flags demand it
- Auditing every tool invocation with actor, subject, and outcome

## Usage modes

### 1. Allow-list policy (tests and prototypes)

Configure `AllowListPolicy` maps directly when you do not need principal/tenant auth yet:

```go
policy := ai.NewAllowListPolicy()
policy.Read["Patient"] = ai.ReadTypePolicy{}
policy.Search["Patient"] = ai.SearchTypePolicy{
    AllowedParams: []string{"name"},
    MaxCount:      50,
}
exec, err := ai.NewExecutor(ai.Config{
    Resources: resourceStore,
    Search:    searchSvc,
    Views:     viewExec,
    Core:      coreSvc,
    Policy:    policy,
})
```

### 2. Auth-backed policy (`AIPolicyAdapter`)

Use when decisions must match human users, roles, and tenant policy DSL:

```go
exec, err := ai.NewExecutor(ai.Config{
    Policy: &auth.AIPolicyAdapter{
        Engine: authEngine,
        TenantID: "tenant-a",
        Resolve: resolveActor,
        Constraints: aiConstraints,
    },
    Audit: &ai.AuditStoreAdapter{Store: auditStore},
    AuditRequired: true,
})
```

### 3. Approval-gated writes

Enable `CreateApproval` / `UpdateApproval` on write policies and wire `Approval` + `ApprovalStore`:

```go
exec, err := ai.NewExecutor(ai.Config{
    Policy:        policyWithApproval,
    Approval:      myApprovalHook,
    ApprovalStore: tokenStore,
    RequireConversationID: true,
})
res, err := exec.ExecuteTool(ctx, ai.ToolRequest{
    ToolName: ai.ToolWriteFhirResource,
    Input: map[string]any{
        "operation": "create",
        "resourceType": "Patient",
        "fields": map[string]any{"name": []any{map[string]any{"family": "Smith"}}},
    },
})
// res may indicate approval-required with a pending token
```

### 4. De-identified context

Set `Deidentify: true` on read/search/view policies and provide an explicit `Deidentifier`:

```go
exec, err := ai.NewExecutor(ai.Config{
    Policy:       deidPolicy,
    Deidentify:   ai.NewFHIRDeidentifier(ai.DefaultPHICatalog()),
})
```

`Executor` uses a shared `FHIRDeidentifier` (`PHIModeStandard`, `EvalModeKeywordsOnly`)
when `Config.Deidentify` is nil, warms path indexes for common resource types at
startup, and passes `ProfileCatalog` when set. Override with `PHIMode`, `EvalMode`,
or a custom `Deidentifier`.

`FHIRDeidentifier` applies:

- **Compiled FHIRPath** expressions (validated at index build; segment redaction at runtime)
- **StructureDefinition-driven paths** when `ProfileCatalog` / `Executor.ProfileCatalog` is set (base type SD plus **`meta.profile`** URLs on each resource instance)
- **FHIRPath keyword elements** (for example `text.`div``) compiled and evaluated via `pkg/fhirpath`, with JSON segment redaction ordered deepest-first
- **PHICatalog** path suffixes and passive element names at any depth
- **`meta.security`** labels (v3 confidentiality and HL7 security-labels by default)

Customize `PHICatalog`, `PHIStructureRules`, or implement `Deidentifier` for site-specific rules.

```go
snapshot, _ := manager.RebuildSnapshot(ctx)
exec, _ := ai.NewExecutor(ai.Config{
    Policy:         policy,
    ProfileCatalog: validate.NewRegistryProfileCatalog(snapshot),
})
```

The executor refuses to silently skip de-identification when policy requires it.

### 5. Model routing (optional)

Tools work without a router; add `ModelRouter` when some prompts should hit local vs cloud adapters:

```go
exec, err := ai.NewExecutor(ai.Config{
    ModelRouter: &ai.ModelRouter{Local: localLLM, Cloud: cloudLLM},
})
resp, err := exec.InvokeModel(ctx, ai.ToolRequest{ModelHint: "cloud"}, prompt, toolResult.Context)
```

### 6. Registry-only discovery

Enumerate descriptors without constructing a full executor:

```go
reg := ai.NewRegistry()
for _, tool := range reg.AllToolDescriptors() {
    _ = tool.Name
    _ = tool.InputKeys
}
```

## Examples

**Configure policy and executor:**

```go
policy := ai.NewAllowListPolicy()
policy.Read["Patient"] = ai.ReadTypePolicy{}
policy.Search["Patient"] = ai.SearchTypePolicy{
    AllowedParams: []string{"name"},
    AllowedFields: []string{"name", "gender"},
    MaxCount:      50,
}
policy.Views["patient_summary_view"] = ai.ViewTypePolicy{}
policy.Write["Patient"] = ai.WriteTypePolicy{
    CreateFields:   []string{"name", "gender"},
    UpdateFields:   []string{"name"},
    CreateApproval: true,
}

exec, err := ai.NewExecutor(ai.Config{
    Resources: resourceStore,
    Search:    searchSvc,
    Views:     viewExec,
    Core:      coreSvc,
    Policy:    policy,
    Audit:     &ai.AuditStoreAdapter{Store: auditStore},
    AuditRequired: true,
    RequireConversationID: true,
    Approval:  myApprovalHook,
    ApprovalStore: approvalStore,
})
```

**Execute a generic read:**

```go
res, err := exec.ExecuteTool(ctx, ai.ToolRequest{
    ToolName: ai.ToolReadFhirResource,
    Actor:    "agent-1",
    Subject:  "patient/pat-1",
    Input: map[string]any{
        "resourceType": "Patient",
        "id":           "pat-1",
    },
})
// res.Data, res.Context, res.Citations, res.AuditMeta
```

**Search with bounded paging:**

```go
res, err := exec.ExecuteTool(ctx, ai.ToolRequest{
    ToolName: ai.ToolSearchFhirResources,
    Actor:    "agent-1",
    Input: map[string]any{
        "resourceType": "Patient",
        "params":       map[string]any{"name": "Jane"},
        "count":        25,
    },
})
```

**Run a view for structured rows:**

```go
res, err := exec.ExecuteTool(ctx, ai.ToolRequest{
    ToolName: ai.ToolRunView,
    Actor:    "agent-1",
    Input: map[string]any{
        "viewName": "patient_summary_view",
        "limit":    10,
    },
})
```

**Convenience wrapper:**

```go
res, err := exec.ExecuteTool(ctx, ai.ToolRequest{
    ToolName: "get_patient_summary",
    Actor:    "agent-1",
    Input:    map[string]any{"patientId": "pat-1"},
})
```

## Tool input reference

### `read_fhir_resource`

| Field | Required | Description |
|-------|----------|-------------|
| `resourceType` | yes | FHIR resource type |
| `id` | yes | Resource id |

### `search_fhir_resources`

| Field | Required | Description |
|-------|----------|-------------|
| `resourceType` | yes | FHIR resource type |
| `params` | no | Map of search parameter name to string values |
| `count` | no | Page size; clamped by policy `MaxCount` |
| `offset` | no | Result offset |

### `run_view`

| Field | Required | Description |
|-------|----------|-------------|
| `viewName` | yes | Registered view name |
| `version` | no | View version; defaults when unambiguous |
| `parameters` | no | Passed to view auth/audit |
| `limit` | no | Max rows returned |
| `offset` | no | Row offset |

### `write_fhir_resource`

| Field | Required | Description |
|-------|----------|-------------|
| `operation` | yes | `create` or `update` |
| `resourceType` | yes | FHIR resource type |
| `id` | update only | Existing resource id |
| `fields` | yes | Approved top-level FHIR fields |

Writes do not accept arbitrary FHIR JSON or PATCH documents.

## Safety model

`AllowListPolicy` denies by default:

- Unlisted resource types cannot be read, searched, or written
- Unlisted views cannot be executed
- Search requests containing any parameter not on the allow-list are denied
- Search results expose only `resourceType`/`id` unless `AllowedFields` or `AllowAllFields` is configured
- Write fields not on the allow-list are rejected
- `SearchTypePolicy.MaxCount` bounds page size
- `_include` and `_revinclude` directives require exact policy allow-list entries

Approval is policy-driven via `WriteTypePolicy.CreateApproval` /
`UpdateApproval`. Pending writes use `ApprovalStore` tokens; approved tokens are
verified and consumed before a write is committed. An approval hook that returns
an approved result must also return a token backed by that store.

De-identification is policy-driven via `ReadTypePolicy.Deidentify`,
`SearchTypePolicy.Deidentify`, and `ViewTypePolicy.Deidentify`. When any of
these flags is enabled, an explicit `Deidentifier` must be configured; the
executor will not silently use pass-through behavior.

## Citations and audit

Citations attach provenance for model grounding:

- Resource refs (`Patient/pat-1`) for reads and search matches
- View name, version, and columns for `run_view`
- Search parameter names for `search_fhir_resources`
- Written resource ref and operation for `write_fhir_resource`

Audit records capture actor, subject, tool name, outcome, and request scope.
Outcomes include `success`, `denied`, `validation-failed`, and
`approval-required`.

## Where it fits

| Layer | Role |
|-------|------|
| **ai** | Policy-governed tool harness (this package) |
| **view** | Structured projections for `run_view` |
| **search** | Parameterized lookup for `search_fhir_resources` |
| **core** | Validated writes for `write_fhir_resource` |
| **validate** | Structural validation on write path |
| **auth** | `AIPolicyAdapter` implements `PolicyEngine` with principal/tenant decisions; optional decision audit via `pkg/audit` |
| **audit** | Shared audit event library used by AI `AuditStoreAdapter` |

## Testing

```bash
go test ./pkg/ai/... -count=1
```

Use `pkg/testkit/aitest` for executor harnesses with optional search, views, and approval fakes.

## Limits

- Generic tools only; no raw FHIR server passthrough
- Writes use structured field maps, not full resource JSON or PATCH
- Search scope is allow-listed even when more registry params exist
- In-memory tool registry; persistent tool catalogs are future work
- Model invocation is optional and separate from tool execution
- No prompt orchestration bundled into every flow

## Related docs

- [pkg/auth/README.md](../auth/README.md) — policy adapter
- [pkg/view/README.md](../view/README.md) — `run_view` backend
- [pkg/audit/README.md](../audit/README.md) — tool audit events
- [examples/ai-authz](../../examples/ai-authz/main.go) — runnable sample
- [doc.go](./doc.go) — integration points
