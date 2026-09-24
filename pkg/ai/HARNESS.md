# Agent harness (pkg/ai)

Upstream apps should pass a **model endpoint + optional system prompt** and not
think about FHIR. The harness is a second layer on top of today's `Executor`, not
a replacement for tools or policy.

## Architecture

```text
App: model endpoint + optional system prompt
            │
            ▼
     Agent / Harness (this layer)
  - conversation turn loop
  - tool-calling protocol (OpenAI-compatible function tools)
  - FHIR ↔ JSON context (via Executor ContextFormatter today)
            │
            ▼
     Executor (existing)
  - policy, tools, citations, audit, FHIRDeidentifier
            │
            ▼
     view / search / core / validate
```

## Harness responsibilities

| Area | v1 behavior |
|------|-------------|
| **Session** | `conversationId` + in-memory `[]ChatMessage`; persistence is optional later |
| **Model seam** | `ChatModel` + `OpenAICompatibleAdapter(baseURL, apiKey, model)`; also implements `ModelAdapter` for `InvokeModel` |
| **Orchestration** | `Harness.Chat`: user message → model → tool calls → `ExecuteTool` → feed tool results → repeat until text answer |
| **FHIR ergonomics** | Tool results use executor `Context` JSON; markdown narration is Phase B |
| **Safety defaults** | All reads/writes go through executor policy, de-identification, and audit — unchanged |

## Intentionally outside the harness

- OAuth / SMART login
- Long-term chat storage
- Parsing arbitrary model-generated FHIR JSON (commits only via `write_fhir_resource` inputs)
- Full "answer for FHIR" validation of free-form model output

## Phased rollout

| Phase | Deliverable | Status |
|-------|-------------|--------|
| **A** | OpenAI-compatible adapter + `Harness.Chat(ctx, userMsg)` with tool loop | **This note + code** |
| **B** | Multi-turn emphasis + markdown context builder on view/search rows | Planned |
| **C** | Optional "create patient from conversation" helper → `write_fhir_resource` only | Planned |

## Model integration choice

**Prefer OpenAI-compatible HTTP first.** Most providers (OpenAI, Azure, vLLM,
Ollama shim, LiteLLM) expose `/v1/chat/completions` with function tools. That
matches how upstream apps already configure "URL + API key + model".

Keep **local-only stubs** (like `research/ai-pipeline` `StubModel`) for offline
demos and CI; wire them through `ModelRouter.Local` on `Executor.InvokeModel` or
implement `ChatModel` on a test fake when exercising `Harness.Chat` without HTTP.

## Minimal example

```go
adapter, err := ai.NewOpenAICompatibleAdapter(ai.OpenAICompatibleConfig{
    BaseURL: os.Getenv("OPENAI_BASE_URL"), // e.g. https://api.openai.com
    APIKey:  os.Getenv("OPENAI_API_KEY"),
    Model:   "gpt-4o-mini",
})
exec, err := ai.NewExecutor(/* policy + stores */)
h, err := ai.NewHarness(ai.HarnessConfig{
    Executor:     exec,
    Model:        adapter,
    SystemPrompt: "You are a clinical assistant. Use tools for FHIR data.",
    Actor:        "agent-1",
})
h.SetConversationID("conv-123")
res, err := h.Chat(ctx, "Summarize patient pat-1")
// res.Answer, res.ToolResults
```

See `harness_test.go` for a **fake HTTP** chat server that returns a tool call
then a final answer (no network required in CI).
