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
| Structured writes (any resource) | `ResourceWriteDraft`, `ResourceWritePlan`, `propose_write_resource`, `propose_write_plan`, `CommitWrite` / `CommitWritePlan` (transaction bundle; host adds Provenance, not the model) |
| No arbitrary FHIR JSON commits | `BlockDirectWriteTools` hides write tools; **create** uses field maps, **update** uses FHIRPath `patches` only |
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

When `SessionCompaction.MaxContextTokens` is set, the harness estimates **tokens** for the active model context (checkpoint summary + tail after `lastCoveredEventId`, plus system prompt and tool schemas). Compaction runs when estimated tokens exceed `MaxContextTokens - CompactHeadroomTokens` (default headroom 2048). It summarizes older active events (custom `Summarizer` or default `ChatModel`), then appends a **`compaction` author** checkpoint event. Prior events remain in the database (append-only).

**Tokenizer (OpenAI-compatible models):**

```go
counter, err := ai.NewTiktokenContextCounter(ai.TiktokenCl100kBase) // or TiktokenO200kBase
SessionCompaction: ai.SessionCompactionConfig{
    MaxContextTokens:      100_000,
    CompactHeadroomTokens: 4096,
    TokenCounter:          counter,
},
```

`GetSession` with `GetSessionConfig.ActiveContextOnly: true` (used automatically by the harness) returns only the latest checkpoint row and logical tail events for model context. Use `Harness.LoadFullEventLog` for the complete audit log.

`OnMetric` fires only on successful compaction or failure (not every under-limit check). `Harness.CompactionMetrics()` exposes cumulative counters on the harness instance.

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

## Grounding and anti-hallucination

The harness sits on top of `Executor` tools so answers are tied to **read / search / view / structured write** paths—not free-form FHIR JSON.

| Layer | What it does |
|-------|----------------|
| **Executor** | Policy, validation, citations on every tool, audit, de-id, approval tokens on writes |
| **Harness prompts** | `GroundingConfig` appends `DefaultFHIRGroundingSystemPrompt` (use tools for facts; no invented ids) |
| **Post-checks** | `AnalyzeAnswerGrounding` warns when the answer cites `Resource/id` literals missing from `ChatResult.Citations` |
| **Clinical values** | `AnalyzeClinicalValueGrounding` flags numbers/units (e.g. `142 mg/dL`) not present in tool JSON |
| **Search params** | `PreflightSearchPolicy` (strict) runs policy before `search_fhir_resources`; `AnalyzeSearchParamGrounding` flags narrative search params not used in tool calls (including when no search ran) |
| **Strict mode** | `GroundingMode: strict` fails the turn if **any** `GroundingWarnings` entry is present after all checks (one final gate—not separate per-check returns) |
| **Invocation replay** | Retries with the same `InvocationID` rebuild tool evidence from session events and run the same grounding checks |

```go
Grounding: ai.GroundingConfig{Mode: ai.GroundingStandard}, // default
// Production agents handling chart questions:
Grounding: ai.GroundingConfig{Mode: ai.GroundingStrict},
BlockDirectWriteTools: true,
EnableProposeWriteHelper: true,
ToolContextFormat: ai.ToolContextMarkdown,
```

`ChatResult.GroundingWarnings` lists non-fatal issues in standard mode. Writes still require host approval via `PendingApprovals` + `ExecuteHarnessTool` with `ApprovalToken`.

**Preflight search (strict):** In `GroundingStrict`, the harness calls `policy.CheckSearch` before the executor so disallowed parameters surface as tool errors in the model loop. In standard mode only the executor enforces policy (no duplicate preflight).

`HarnessGroundingGuardrails(hcfg)` complements `HarnessExecutorGuardrails` for wiring checks.

## Host confirmation before commit

All FHIR write tool paths through the harness share the same gate: `Chat` tool execution, `ExecuteHarnessTool`, and `CommitWrite` / `CommitWriteFromSession`. Enable an explicit host callback:

```go
h, _ := ai.NewHarness(ai.HarnessConfig{
    RequireCommitConfirmation: true,
    CommitWritePlanConfirm: func(ctx context.Context, plan ai.ResourceWritePlan) error {
        // UI / workflow approval for single- or multi-entry plans; return nil to allow commit
        return nil
    },
})
_, err := h.CommitWrite(ctx, draft) // ErrCommitNotConfirmed when hook missing or returns error
```

Use `CommitWriteWithOptions(ctx, draft, ai.CommitWriteOptions{SkipHostConfirm: true})` only in tests or trusted automation. Pass `CommitWriteOptions.ApprovalToken` to resume policy-gated commits after `ApprovalStore` approval.

During **`Chat`**, when `RequireCommitConfirmation` is set, the harness runs `CommitWritePlanConfirm` (via `confirmBeforeWriteToolInput`) **before** calling the executor; the internal bundle commit then uses `SkipHostConfirm` so the host callback is not invoked twice.

**Policy write approval vs host confirm:** `ApprovalStore` / `WriteTypePolicy` tokens apply to `execute_fhir_bundle`, including harness `CommitWrite` / `CommitWritePlan` and normalized harness write tools (create/update are committed as transaction bundles). Host UI confirmation is separate via `CommitWritePlanConfirm`.

**Retry shapes:** `ChatResult.PendingApprovals` sets `Input` to the normalized **`execute_fhir_bundle`** payload (not shown in tool messages to the model). Retry with `ExecuteHarnessTool` using `pending.ToolName`, `pending.Input`, and `pending.Token`. For **`CommitWritePlan`** called directly by the host, keep the same `ResourceWritePlan` and pass `CommitWriteOptions.ApprovalToken` — `PendingApprovals.Input` is only populated for Chat-normalized writes.

**Plans and batch reads:** `ResourceWritePlan` supports `read` entries (batch `GET`) alongside creates/updates. `CommitWritePlanConfirm` sees the full plan including reads; commits use `bundleType=batch` when reads are present.

## AI Transparency (HL7 AI on FHIR IG)

When `Executor` `Config.AIAttribution.Enabled` is true, successful AI-mediated creates/updates:

1. Stamp `meta.security` with **AIAST** (`http://terminology.hl7.org/CodeSystem/v3-ObservationValue`) per [AI Transparency requirements](https://build.fhir.org/ig/HL7/aitransparency-ig/en/requirements.html).
2. Add `meta.extension` (`urn:haistack:fhir:StructureDefinition:ai-agent-context`) with `conversationId` / `actor`—**does not overwrite** clinical `meta.source`.
3. Create a **Provenance** resource targeting the written resource (optional via `CreateProvenance`, default on). Provenance uses R4 `CodeableConcept` for `entity.role`.

**Provenance is best-effort by default** (`ProvenanceBestEffort`, default true) on direct `create_fhir_resource` / `update_fhir_resource` calls. **Harness** `CommitWrite` / `CommitWritePlan` commit via `execute_fhir_bundle` (transaction by default, batch when the plan includes reads). Harness does **not** read `AtomicProvenance`. System Provenance is not auto-appended for a clinical target when the bundle already includes a `Provenance` POST covering that target.

**AtomicProvenance** (executor config only): when true, direct single create/update tools use a one-entry transaction bundle plus Provenance instead of a separate Provenance `Create`.

Provenance is created via `Core.Create` (system side-effect, not create/update tool policy). Ensure the core store allows `Provenance` creates for the executor principal. Bundle entries may include `Provenance` POSTs; when attribution is enabled the executor may still append its own Provenance for clinical writes.

Wire attribution on the same executor used by the harness; pass `ConversationID` on writes via session (`Harness` sets it from the active session).

```go
exec, _ := ai.NewExecutor(ai.Config{
    // ...
    AIAttribution: ai.AIAttributionConfig{
        Enabled: true,
        AgentDisplay: "My Clinical Agent",
        ModelID:      "gpt-4.1",
    },
})
```

## Why not validate arbitrary FHIR JSON?

Models can hallucinate invalid or unsafe resources. The executor **does not** accept
full Resource JSON from model text. Creates use `create_fhir_resource` with an allow-listed **fields** map;
updates use `update_fhir_resource` with **patches** keyed by FHIR Patch paths (e.g. `name[0].family`), not
FHIRPath functions such as `Patient.name.where(...)`. Policy `UpdateFields` must list patch keys exactly.
Set `RequireValidatorOnWrites` on the executor to require `pkg/validate` on every commit.
The harness reinforces structured writes with `BlockDirectWriteTools` + `propose_write_resource` /
`propose_write_plan` → `CommitWrite` / `CommitWritePlan`. Prefer field maps and patch paths so
policy and validation stay explicit; full Resource JSON in bundles is not the primary v1 shape.

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
        ToolName:      pending.ToolName,
        Input:         pending.Input,
        ApprovalToken: pending.Token,
    })
}
```

### Patient create from assistant text

```go
if draft, ok := ai.ExtractResourceWriteDraft(h.Session().Messages); ok {
    _, _ = h.CommitWrite(ctx, draft)
}
```
