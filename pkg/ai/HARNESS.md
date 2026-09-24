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
| Structured writes (any resource) | `ResourceWriteDraft`, `propose_write_resource`, `CommitWrite` / `CommitWriteFromSession` (`PatientCreateDraft` is a convenience wrapper) |
| No arbitrary FHIR JSON commits | `BlockDirectWriteTools` hides `write_fhir_resource` from the model; commits go through validated maps |
| Policy + de-id + audit | Unchanged on `Executor`; use `HarnessExecutorGuardrails` when wiring production agents |
| Approval-gated writes | `ChatResult.PendingApprovals`; resume with `ExecuteHarnessTool` + `ApprovalToken` |
| Citations for grounding | Per-tool `ToolResult.Citations`; aggregated on `ChatResult.Citations` |
| Long-term chat storage | **Out of scope today** — see [Conversation storage](#conversation-storage) |
| Free-form FHIR answer validation | **Out of scope** — see [Why not validate arbitrary FHIR JSON?](#why-not-validate-arbitrary-fhir-json) |
| OAuth / SMART login | **Out of scope** — see [Why OAuth/SMART is outside the harness](#why-oauthsmart-is-outside-the-harness) |
| Prompt-only JSON tool protocol | **Not implemented** — see [Tool-calling protocols](#tool-calling-protocols) |

## Conversation storage

**Today:** `Harness` keeps `Session.Messages` and `Session.ConversationID` **in memory only**
inside the process. `SetConversationID` / `AutoConversationID` correlate executor audit
and policy checks; nothing is written to SQLite/Postgres by the harness itself.

**Proposed v2 seam (not built yet):**

```text
type ConversationStore interface {
    Load(ctx, conversationID) (Session, error)
    Save(ctx, Session) error
}
```

- Host app implements storage (tenant DB, encrypted blob store, etc.).
- `NewHarnessWithSession` loads on turn 1; `Chat` appends and optionally `Save` after each turn.
- Retention, encryption, and HIPAA boundaries stay in the app — same as OAuth.

Until that interface lands, apps should persist `ChatResult.Messages` themselves if they
need cross-request continuity beyond a single `Harness` instance.

## Tool-calling protocols

**Native function tools (what we use now):** the model API exposes JSON-schema tools
(`ChatToolsFromDescriptors` → OpenAI `/v1/chat/completions` `tools[]`). The provider
parses structured `tool_calls` with names and JSON arguments. The harness executes
those calls and returns `role: tool` messages.

**Prompt-only JSON (future / alternate):** some local models have no tool channel. The
app would instruct the model to emit a JSON blob in text (e.g. `{"tool":"read_fhir_resource",...}`),
then the harness would parse that blob and run `ExecuteTool`. Same safety boundaries,
different transport. We have not implemented that parser loop yet because OpenAI-compatible
HTTP covers most deployment targets.

## Why not validate arbitrary FHIR JSON?

Models can hallucinate invalid or unsafe resources. The executor **does not** accept
full Resource JSON from model text. Writes must go through `write_fhir_resource` with
an allow-listed **field map** so policy, validation, and approval run on known keys.
The harness reinforces that with `BlockDirectWriteTools` + `propose_write_resource` →
`CommitWrite`. Validating arbitrary generated FHIR would duplicate `pkg/validate` on
untrusted blobs and still bypass field-level policy.

## Why OAuth/SMART is outside the harness

The harness assumes the **host has already authenticated the user or service** and
passes `HarnessConfig.Actor`, `Subject`, and tenant into `Executor`. SMART/OAuth
(login, consent, scopes, token refresh) belongs in `pkg/smart`, `pkg/http`, and app
middleware — the same place you would set the actor before calling `Harness.Chat`.
Mixing OAuth into `pkg/ai` would couple every agent deployment to one auth shape.

## Phased rollout

| Phase | Deliverable | Status |
|-------|-------------|--------|
| **A** | OpenAI-compatible adapter + `Harness.Chat` tool loop | Done |
| **B** | Multi-turn session + markdown tool context | Done |
| **C** | Structured write proposal + commit helpers (any resource type) | Done |

## Production wiring checklist

1. `Executor` with `Policy`, `Audit` + `AuditRequired: true`, and `Deidentifier` when policy sets `Deidentify`.
2. `RequireConversationID` on executor → enable `HarnessConfig.AutoConversationID` or set ID before `Chat`.
3. `HarnessExecutorGuardrails(cfg)` — log/alert on missing seams.
4. `BlockDirectWriteTools: true` when using `EnableProposeWriteHelper` or app-side commit paths.
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
    EnableProposeWriteHelper: true,
})
res, _ := h.Chat(ctx, "Summarize the patient")
// res.Answer, res.Citations, res.PendingApprovals

draft := ai.ResourceWriteDraft{
    Operation: ai.WriteOperationCreate, ResourceType: "Patient",
    Fields: map[string]any{"name": []any{map[string]any{"family": "Lee", "given": []string{"Sam"}}}},
}
_, _ = h.CommitWrite(ctx, draft)
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
if draft, ok := ai.ExtractResourceWriteDraft(h.Session().Messages); ok {
    _, _ = h.CommitWrite(ctx, draft)
}
```
