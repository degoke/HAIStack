# haistack-ai (`pkg/ai`)

Policy-governed FHIR AI gateway for Health AI Stack. LLMs call typed,
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

## Usage

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

**Discover tools for a model:**

```go
reg := ai.NewRegistry()
for _, tool := range reg.AllToolDescriptors() {
    // tool.Name, tool.Description, tool.Generic, tool.InputKeys
}
```

**Optional model routing (tools work without this):**

```go
exec, err := ai.NewExecutor(ai.Config{
    // ...
    ModelRouter: &ai.ModelRouter{
        Local: localAdapter,
        Cloud: cloudAdapter,
    },
})

resp, err := exec.InvokeModel(ctx, ai.ToolRequest{ModelHint: "cloud"}, prompt, res.Context)
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

## MVP limits

- Generic tools only; no raw FHIR server passthrough
- Writes use structured field maps, not full resource JSON or PATCH
- Search scope is allow-listed even when more registry params exist
- In-memory tool registry; persistent tool catalogs are future work
- Model invocation is optional and separate from tool execution
- No prompt orchestration bundled into every flow

See [doc.go](./doc.go) for the full API, package boundaries, and integration
points.
