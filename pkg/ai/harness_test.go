package ai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
)

func TestHarness_ChatToolLoopWithFakeHTTPModel(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:     true,
		allowPatientRead: true,
	})

	var step int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		step++
		current := step
		mu.Unlock()

		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = body

		w.Header().Set("Content-Type", "application/json")
		switch current {
		case 1:
			_, _ = w.Write([]byte(`{
				"choices":[{
					"finish_reason":"tool_calls",
					"message":{
						"role":"assistant",
						"content":"",
						"tool_calls":[{
							"id":"call_1",
							"type":"function",
							"function":{
								"name":"read_fhir_resource",
								"arguments":"{\"resourceType\":\"Patient\",\"id\":\"pat-jane\"}"
							}
						}]
					}
				}]
			}`))
		default:
			_, _ = w.Write([]byte(`{
				"choices":[{
					"finish_reason":"stop",
					"message":{"role":"assistant","content":"Patient pat-jane is in the record."}
				}]
			}`))
		}
	}))
	defer srv.Close()

	adapter, err := ai.NewOpenAICompatibleAdapter(ai.OpenAICompatibleConfig{
		BaseURL: srv.URL,
		Model:   "fake",
		Name:    "fake-http",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter: %v", err)
	}

	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor: h.exec,
		Model:    adapter,
		Actor:    "agent-1",
	})
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}

	res, err := harness.Chat(context.Background(), "Who is pat-jane?")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Answer != "Patient pat-jane is in the record." {
		t.Fatalf("answer = %q", res.Answer)
	}
	if len(res.ToolResults) != 1 {
		t.Fatalf("tool results = %d, want 1", len(res.ToolResults))
	}
	if res.ToolResults[0].Err != nil {
		t.Fatalf("tool err: %v", res.ToolResults[0].Err)
	}
	if res.ToolResults[0].ToolName != ai.ToolReadFhirResource {
		t.Fatalf("tool name = %q", res.ToolResults[0].ToolName)
	}
}

func TestOpenAICompatibleAdapter_InvokeLegacy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}]
		}`))
	}))
	defer srv.Close()

	adapter, err := ai.NewOpenAICompatibleAdapter(ai.OpenAICompatibleConfig{
		BaseURL: srv.URL,
		Model:   "fake",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter: %v", err)
	}
	resp, err := adapter.Invoke(context.Background(), ai.ModelRequest{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("content = %q", resp.Content)
	}
}

func TestChatToolsFromDescriptors(t *testing.T) {
	tools := ai.ChatToolsFromDescriptors(ai.GenericToolDescriptors())
	if len(tools) != 4 {
		t.Fatalf("tools = %d", len(tools))
	}
	if tools[0].Name != ai.ToolReadFhirResource {
		t.Fatalf("first tool = %q", tools[0].Name)
	}
}

func TestParseToolArguments(t *testing.T) {
	input, err := ai.ParseToolArguments(`{"resourceType":"Patient","id":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if input["id"] != "x" {
		t.Fatalf("id = %v", input["id"])
	}
}

func TestOpenAICompatibleAdapter_RequestShape(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	adapter, _ := ai.NewOpenAICompatibleAdapter(ai.OpenAICompatibleConfig{
		BaseURL: srv.URL,
		Model:   "m",
	})
	_, err := adapter.Chat(context.Background(), ai.ChatRequest{
		SystemPrompt: "sys",
		Messages:     []ai.ChatMessage{{Role: ai.ChatRoleUser, Content: "hi"}},
		Tools:        ai.ChatToolsFromDescriptors(ai.GenericToolDescriptors()),
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := captured["messages"].([]any)
	if len(msgs) < 2 {
		t.Fatalf("messages = %v", captured["messages"])
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" || !strings.Contains(first["content"].(string), "sys") {
		t.Fatalf("system message missing: %v", first)
	}
}

func TestHarness_MultiTurnSessionRetainsHistory(t *testing.T) {
	model := &recordingChatModel{responses: []*ai.ChatResponse{
		{Content: "first answer"},
		{Content: "second answer"},
	}}
	h := newTestHarness(t, harnessOptions{})
	harness, err := ai.NewHarness(ai.HarnessConfig{Executor: h.exec, Model: model, Actor: "agent-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = harness.Chat(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	_, err = harness.Chat(context.Background(), "follow up")
	if err != nil {
		t.Fatal(err)
	}
	if len(model.requests) != 2 {
		t.Fatalf("model calls = %d", len(model.requests))
	}
	if len(model.requests[1].Messages) < 3 {
		t.Fatalf("second request missing prior turns: %d messages", len(model.requests[1].Messages))
	}
}

func TestHarness_InvalidToolArgumentsReturnedToModel(t *testing.T) {
	model := &recordingChatModel{responses: []*ai.ChatResponse{
		{ToolCalls: []ai.ChatToolCall{{
			ID: "c1", Name: ai.ToolReadFhirResource, Arguments: "not-valid-json",
		}}},
		{Content: "I will fix the tool call."},
	}}
	h := newTestHarness(t, harnessOptions{})
	harness, err := ai.NewHarness(ai.HarnessConfig{Executor: h.exec, Model: model, Actor: "agent-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = harness.Chat(context.Background(), "read patient")
	if err != nil {
		t.Fatal(err)
	}
	if len(model.requests) < 2 {
		t.Fatalf("expected second model round, got %d", len(model.requests))
	}
	var sawToolError bool
	for _, m := range model.requests[1].Messages {
		if m.Role == ai.ChatRoleTool && strings.Contains(m.Content, "arguments") {
			sawToolError = true
			break
		}
	}
	if !sawToolError {
		t.Fatalf("second model request missing tool error message: %+v", model.requests[1].Messages)
	}
}

func TestHarness_MarkdownToolContext(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:     true,
		allowPatientRead: true,
	})
	model := &recordingChatModel{responses: []*ai.ChatResponse{
		{ToolCalls: []ai.ChatToolCall{{
			ID: "c1", Name: ai.ToolReadFhirResource,
			Arguments: `{"resourceType":"Patient","id":"pat-jane"}`,
		}}},
		{Content: "done"},
	}}
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor:          h.exec,
		Model:             model,
		Actor:             "agent-1",
		ToolContextFormat: ai.ToolContextMarkdown,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = harness.Chat(context.Background(), "read jane")
	if err != nil {
		t.Fatal(err)
	}
	var toolMsg string
	for _, m := range harness.Session().Messages {
		if m.Role == ai.ChatRoleTool {
			toolMsg = m.Content
		}
	}
	if !strings.Contains(toolMsg, "### Patient/pat-jane") {
		t.Fatalf("expected markdown read context, got: %s", toolMsg)
	}
}

func TestHarness_AutoConversationID(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:     true,
		allowPatientRead: true,
	})
	h.exec, _ = ai.NewExecutor(ai.Config{
		Resources:             h.resources,
		Policy:                h.policy,
		RequireConversationID: true,
	})
	model := &recordingChatModel{responses: []*ai.ChatResponse{{Content: "ok"}}}
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor:           h.exec,
		Model:              model,
		Actor:              "agent-1",
		AutoConversationID: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = harness.Chat(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if harness.Session().ConversationID == "" {
		t.Fatal("expected auto conversation id")
	}
}

func TestHarness_BlockDirectWriteTools(t *testing.T) {
	h := newTestHarness(t, harnessOptions{})
	model := &recordingChatModel{responses: []*ai.ChatResponse{{Content: "ok"}}}
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor:              h.exec,
		Model:                 model,
		Actor:                 "agent-1",
		BlockDirectWriteTools: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = harness.Chat(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range model.requests[0].Tools {
		if tool.Name == ai.ToolWriteFhirResource {
			t.Fatal("write tool should be blocked")
		}
	}
}

func TestHarness_ChatAggregatesCitations(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:     true,
		allowPatientRead: true,
	})
	model := &recordingChatModel{responses: []*ai.ChatResponse{
		{ToolCalls: []ai.ChatToolCall{{
			ID: "c1", Name: ai.ToolReadFhirResource,
			Arguments: `{"resourceType":"Patient","id":"pat-jane"}`,
		}}},
		{Content: "done"},
	}}
	harness, _ := ai.NewHarness(ai.HarnessConfig{Executor: h.exec, Model: model, Actor: "agent-1"})
	res, err := harness.Chat(context.Background(), "read")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Citations) == 0 {
		t.Fatal("expected citations on chat result")
	}
}

func TestHarness_ProposePatientCreateDoesNotWrite(t *testing.T) {
	h := newTestHarness(t, harnessOptions{withCore: true, allowPatientWrite: true})
	before := len(h.resources.all())
	model := &recordingChatModel{responses: []*ai.ChatResponse{
		{ToolCalls: []ai.ChatToolCall{{
			ID: "c1", Name: ai.ToolProposePatientCreate,
			Arguments: `{"family":"Proposed","given":["Pat"]}`,
		}}},
		{Content: "proposed"},
	}}
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor:                  h.exec,
		Model:                     model,
		Actor:                     "agent-1",
		EnablePatientCreateHelper: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = harness.Chat(context.Background(), "register new patient")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.resources.all()) != before {
		t.Fatal("propose tool should not commit write")
	}
	draft, ok := ai.ExtractPatientCreateDraft([]ai.ChatMessage{})
	if ok {
		t.Fatal("no fenced block expected")
	}
	_ = draft
}

type recordingChatModel struct {
	requests  []ai.ChatRequest
	responses []*ai.ChatResponse
}

func (m *recordingChatModel) Name() string { return "recording" }

func (m *recordingChatModel) Chat(_ context.Context, req ai.ChatRequest) (*ai.ChatResponse, error) {
	m.requests = append(m.requests, req)
	if len(m.responses) == 0 {
		return &ai.ChatResponse{Content: "ok"}, nil
	}
	resp := m.responses[0]
	m.responses = m.responses[1:]
	return resp, nil
}
