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
  - Session (conversationId; events via SessionService when configured)
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
| Session: `conversationId`, events, state | `Session` + `store.SessionService` (ADK-style) |
| Model seam: URL + provider | `OpenAICompatibleAdapter`, `ChatModel`, optional `ModelAdapterChatModel` bridge for local stubs |
| Model routing hint | `HarnessConfig.ModelHint` → `ChatRequest.Hint`; `ModelRouter` still used via `Executor.InvokeModel` |
| Orchestration loop | `Harness.Chat`: user → model → tools → `ExecuteTool` → tool messages → repeat until text |
| Tool protocol | **Native** function tools (`ToolCallProtocolNative`), **prompt JSON** (`ToolCallProtocolPromptJSON`), or **both** (`ToolCallProtocolBoth`) |
| FHIR narration (markdown) | `ToolContextMarkdown` + `MarkdownContextBuilder` for read/search/view |
| Structured writes (any resource) | `ResourceWriteDraft`, `propose_write_resource`, `CommitWrite` / `CommitWriteFromSession` (`PatientCreateDraft` is a convenience wrapper) |
| No arbitrary FHIR JSON commits | `BlockDirectWriteTools` hides `write_fhir_resource` from the model; commits go through validated maps |
| Policy + de-id + audit | Unchanged on `Executor`; use `HarnessExecutorGuardrails` when wiring production agents |
| Approval-gated writes | `ChatResult.PendingApprovals`; resume with `ExecuteHarnessTool` + `ApprovalToken` |
| Citations for grounding | Per-tool `ToolResult.Citations`; aggregated on `ChatResult.Citations` |
| Long-term chat storage | `store.SessionService` (SQLite/Postgres) — see [Session storage](#session-storage-google-adk-style) |
| Free-form FHIR answer validation | **Out of scope** — see [Why not validate arbitrary FHIR JSON?](#why-not-validate-arbitrary-fhir-json) |
| OAuth / SMART login | **Out of scope** — see [Why OAuth/SMART is outside the harness](#why-oauthsmart-is-outside-the-harness) |

## Session storage (Google ADK-style)

Modeled after [ADK SessionService](https://adk.dev/sessions/session/): a **session** has
`id`, `app_name`, `user_id`, scratchpad **state**, and append-only **events** (user/model/tool
turns). State deltas on events support `app:` and `user:` key prefixes (app/user scoped state tables).

**Store interface:** `pkg/store.SessionService`

| Method | Role |
|--------|------|
| `CreateSession` | New thread |
| `GetSession` | Load state + events (`GetSessionConfig` for recent events / after timestamp) |
| `AppendEvent` | Persist one step (skips `partial` events) |
| `ListSessions` / `DeleteSession` | Metadata lifecycle |
| `GetUserState` / `GetAppState` | Cross-session scratchpads |

**Backends:**

| Backend | Constructor |
|---------|-------------|
| SQLite | `db.SessionService(tenantID)` |
| Postgres | `tdb.SessionService()` |

**Harness wiring:**

```go
h, _ := ai.NewHarness(ai.HarnessConfig{
    SessionService: stack.DB.SessionService("tenant-a"),
    TenantID:       "tenant-a",
    AppName:        "my-agent",
    Actor:          "user-123", // ADK user_id
    AutoConversationID: true,
})
_, _ = h.Chat(ctx, "Hello") // creates session if missing; appends events each turn
```

`LoadAgentSession` / `CreateAgentSession` for explicit control.

With `SessionService` configured, **each `Chat` reloads events from the store** before handling the user message. The database is the only source of truth for transcript and session state (not preloaded `Session` fields). `CommitWriteFromSession` and `ExecuteHarnessTool` also reload from the store when `SessionService` is set.

**Invocation IDs (idempotent turns):** pass a stable id per user turn via `ChatWithOptions`. All events in that turn share `invocationId`. Retrying the same id after a partial failure does not duplicate the user event; resuming continues the tool loop from stored events. A completed turn returns the stored final answer without calling the model again.

```go
inv := uuid.NewString()
res, err := h.ChatWithOptions(ctx, ai.ChatOptions{UserMessage: "…", InvocationID: inv})
// res.InvocationID == inv
```

Invalid tool arguments produce a **tool** message (persisted and included in the next model request), same as executor failures.

### Session compaction (append-only checkpoints)

When `SessionCompaction.MaxContextChars` is set, the harness measures the **active** model context (latest checkpoint summary + events after it). If over the limit, it summarizes older active events using `ChatModel`, then appends a **`compaction` author** checkpoint event. Prior events remain in the database (append-only); `ChatMessagesFromSessionEvents` and `Harness.Session().Messages` only surface the checkpoint summary plus the retained tail.

```go
SessionCompaction: ai.SessionCompactionConfig{
    MaxContextChars:    120_000,
    RetainRecentEvents: 12,
    MinEventsToCompact: 16,
},
```

Use `Harness.PersistedEvents()` for the full audit log including pre-checkpoint events.

## Tool-calling protocols

Set `HarnessConfig.ToolCallProtocol`:

| Value | Behavior |
|-------|----------|
| `native` (default) | OpenAI-style `tools[]` + `tool_calls` on the HTTP API |
| `prompt_json` | Model emits `{"tool":"…","input":{…}}` in text; harness parses and runs `ExecuteTool` |
| `both` | Use native `tool_calls` when present; otherwise parse prompt JSON |

Prompt JSON instructions are appended to the system prompt via `AppendPromptJSONToolInstructions`.
Parsed calls are validated against the same allow-listed tool names as native mode.

```go
h, _ := ai.NewHarness(ai.HarnessConfig{
    ToolCallProtocol: ai.ToolCallProtocolPromptJSON,
    // ...
})
```

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
