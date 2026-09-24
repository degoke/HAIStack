# Agent harness (pkg/ai)

Upstream apps pass a **model endpoint + optional system prompt** and do not implement
FHIR tool orchestration themselves. The harness is a **second layer on top of
`Executor`**, not a replacement for tools, policy, audit, or de-identification.

## Architecture

```text
App: model URL/provider + optional system prompt
            │
            ▼
     Agent / Harness
  - Session (conversationId + in-memory messages)
  - ChatModel / OpenAI-compatible function tools
  - Orchestration loop (model ↔ ExecuteTool)
  - Optional markdown narration + structured write proposals
            │
            ▼
     Executor (existing)
  - policy, tools, citations, audit, Deidentifier
            │
            ▼
     view / search / core / validate
```

## Discussion → implementation map

| Discussed responsibility | API / behavior |
|--------------------------|----------------|
| Session: `conversationId`, message history | `Session`, `SetConversationID`, `NewHarnessWithSession`; in-memory only (no long-term store) |
| Model seam: URL + provider | `OpenAICompatibleAdapter`, `ChatModel`, optional `ModelAdapterChatModel` bridge for local stubs |
| Model routing hint | `HarnessConfig.ModelHint` → `ChatRequest.Hint`; `ModelRouter` still used via `Executor.InvokeModel` |
| Orchestration loop | `Harness.Chat`: user → model → tools → `ExecuteTool` → tool messages → repeat until text |
| Tool protocol | OpenAI-style **function tools** from `Registry.AllToolDescriptors` (`ChatToolsFromDescriptors`) |
| FHIR narration (markdown) | `ToolContextMarkdown` + `MarkdownContextBuilder` for read/search/view |
| Structured writes | `PatientCreateDraft`, `propose_patient_create`, `CommitPatientCreate` / `CommitPatientCreateFromSession` |
| No arbitrary FHIR JSON commits | `BlockDirectWriteTools` hides `write_fhir_resource` from the model; commits go through validated maps |
| Policy + de-id + audit | Unchanged on `Executor`; use `HarnessExecutorGuardrails` when wiring production agents |
| Approval-gated writes | `ChatResult.PendingApprovals`; resume with `ExecuteHarnessTool` + `ApprovalToken` |
| Citations for grounding | Per-tool `ToolResult.Citations`; aggregated on `ChatResult.Citations` |
| OAuth / SMART | **Out of scope** (host app) |
| Long-term chat storage | **Out of scope** (host app / future persistence adapter) |
| Free-form FHIR answer validation | **Out of scope** — only `write_fhir_resource` field maps |

## Phased rollout

| Phase | Deliverable | Status |
|-------|-------------|--------|
| **A** | OpenAI-compatible adapter + `Harness.Chat` tool loop | Done |
| **B** | Multi-turn session + markdown tool context | Done |
| **C** | Patient create proposal + commit helpers | Done |

## Production wiring checklist

1. `Executor` with `Policy`, `Audit` + `AuditRequired: true`, and `Deidentifier` when policy sets `Deidentify`.
2. `RequireConversationID` on executor → enable `HarnessConfig.AutoConversationID` or set ID before `Chat`.
3. `HarnessExecutorGuardrails(cfg)` — log/alert on missing seams.
4. `BlockDirectWriteTools: true` when using `EnablePatientCreateHelper` or app-side commit paths.
5. `ToolContextMarkdown` when models should read view/search rows as tables/lists.

```go
for _, note := range ai.HarnessExecutorGuardrails(execCfg) {
    log.Warn(note)
}
```

## Model integration

**Prefer OpenAI-compatible HTTP** (`OpenAICompatibleAdapter`) for real providers.

For **offline / CI**, use:

- `httptest` fake `/v1/chat/completions` (see `harness_test.go`), or
- `NewModelAdapterChatModel(stub, "local")` wrapping `research/ai-pipeline` `StubModel` (single-turn text, no tool calls from the stub).

## Examples

| Location | What it shows |
|----------|----------------|
| `go run ./examples/ai-harness` | Full `Harness.Chat` with markdown context + scripted `ChatModel` |
| `pkg/ai/harness_test.go` | Fake HTTP model + tool loop |
| `examples/ai-authz` | Executor + policy only (no harness) |

## API quick reference

```go
adapter, _ := ai.NewOpenAICompatibleAdapter(ai.OpenAICompatibleConfig{
    BaseURL: baseURL, APIKey: apiKey, Model: modelID,
})
exec, _ := ai.NewExecutor(/* policy, stores, audit */)
h, _ := ai.NewHarness(ai.HarnessConfig{
    Executor: exec, Model: adapter, Actor: "agent-1",
    SystemPrompt: "...",
    ToolContextFormat: ai.ToolContextMarkdown,
    AutoConversationID: true,
    BlockDirectWriteTools: true,
    EnablePatientCreateHelper: true,
})
res, _ := h.Chat(ctx, "Summarize the patient")
// res.Answer, res.Citations, res.PendingApprovals

draft := ai.PatientCreateDraft{Family: "Lee", Given: []string{"Sam"}}
_, _ = h.CommitPatientCreate(ctx, draft)
```

### Approval resume

```go
res, _ := h.Chat(ctx, "update patient ...")
for _, pending := range res.PendingApprovals {
    _, _ = h.ExecuteHarnessTool(ctx, ai.ToolRequest{
        ToolName: pending.ToolName,
        Input:    /* same write input */,
        ApprovalToken: approvedToken,
    })
}
```

### Patient create from assistant text

```go
if draft, ok := ai.ExtractPatientCreateDraft(h.Session().Messages); ok {
    _, _ = h.CommitPatientCreate(ctx, draft)
}
```
