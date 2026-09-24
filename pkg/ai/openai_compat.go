package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAICompatibleConfig configures an HTTP client for chat/completions-style APIs
// (OpenAI, Azure OpenAI, vLLM, Ollama OpenAI shim, etc.).
type OpenAICompatibleConfig struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
	Name       string
}

// OpenAICompatibleAdapter calls a remote chat completions endpoint with tools support.
type OpenAICompatibleAdapter struct {
	baseURL string
	apiKey  string
	model   string
	name    string
	client  *http.Client
}

// NewOpenAICompatibleAdapter validates config and returns an adapter.
func NewOpenAICompatibleAdapter(cfg OpenAICompatibleConfig) (*OpenAICompatibleAdapter, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("ai: openai-compatible adapter requires BaseURL")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, fmt.Errorf("ai: openai-compatible adapter requires Model")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = "openai-compatible"
	}
	return &OpenAICompatibleAdapter{
		baseURL: base,
		apiKey:  cfg.APIKey,
		model:   model,
		name:    name,
		client:  client,
	}, nil
}

// Name implements ModelAdapter and ChatModel.
func (a *OpenAICompatibleAdapter) Name() string {
	if a == nil {
		return ""
	}
	return a.name
}

// Invoke implements ModelAdapter using a single user turn (legacy Executor.InvokeModel seam).
func (a *OpenAICompatibleAdapter) Invoke(ctx context.Context, req ModelRequest) (*ModelResponse, error) {
	messages := []ChatMessage{{Role: ChatRoleUser, Content: req.Prompt}}
	if strings.TrimSpace(req.Context) != "" {
		messages = []ChatMessage{
			{Role: ChatRoleUser, Content: "Context:\n" + req.Context},
			{Role: ChatRoleUser, Content: req.Prompt},
		}
	}
	resp, err := a.Chat(ctx, ChatRequest{
		Messages: messages,
		Hint:     req.Hint,
	})
	if err != nil {
		return nil, err
	}
	content := resp.Content
	if len(resp.ToolCalls) > 0 {
		var b strings.Builder
		if content != "" {
			b.WriteString(content)
			b.WriteByte('\n')
		}
		for _, tc := range resp.ToolCalls {
			b.WriteString(tc.Name)
			b.WriteString(": ")
			b.WriteString(tc.Arguments)
			b.WriteByte('\n')
		}
		content = b.String()
	}
	return &ModelResponse{Adapter: resp.Adapter, Content: content}, nil
}

// Chat posts one chat completion request.
func (a *OpenAICompatibleAdapter) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if a == nil {
		return nil, fmt.Errorf("ai: nil openai-compatible adapter")
	}
	body, err := json.Marshal(a.buildRequestBody(req))
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if a.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
	httpResp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = httpResp.Body.Close() }()
	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("ai: chat completions HTTP %d: %s", httpResp.StatusCode, truncateForError(raw))
	}
	return a.parseResponse(raw)
}

func (a *OpenAICompatibleAdapter) buildRequestBody(req ChatRequest) map[string]any {
	messages := make([]map[string]any, 0, len(req.Messages)+1)
	if sp := strings.TrimSpace(req.SystemPrompt); sp != "" {
		messages = append(messages, map[string]any{
			"role":    ChatRoleSystem,
			"content": sp,
		})
	}
	for _, m := range req.Messages {
		msg := map[string]any{"role": m.Role}
		if m.Role == ChatRoleTool {
			msg["tool_call_id"] = m.ToolCallID
			msg["content"] = m.Content
		} else {
			if m.Content != "" {
				msg["content"] = m.Content
			}
			if len(m.ToolCalls) > 0 {
				calls := make([]map[string]any, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					calls = append(calls, map[string]any{
						"id":   tc.ID,
						"type": "function",
						"function": map[string]any{
							"name":      tc.Name,
							"arguments": tc.Arguments,
						},
					})
				}
				msg["tool_calls"] = calls
			}
		}
		messages = append(messages, msg)
	}
	out := map[string]any{
		"model":    a.model,
		"messages": messages,
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tool.Name,
					"description": tool.Description,
					"parameters":  tool.Parameters,
				},
			})
		}
		out["tools"] = tools
	}
	return out
}

func (a *OpenAICompatibleAdapter) parseResponse(raw []byte) (*ChatResponse, error) {
	var payload struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if payload.Error != nil && payload.Error.Message != "" {
		return nil, fmt.Errorf("ai: chat completions error: %s", payload.Error.Message)
	}
	if len(payload.Choices) == 0 {
		return nil, fmt.Errorf("ai: chat completions returned no choices")
	}
	choice := payload.Choices[0]
	out := &ChatResponse{
		Adapter:    a.name,
		Content:    choice.Message.Content,
		StopReason: choice.FinishReason,
	}
	for _, tc := range choice.Message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ChatToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return out, nil
}

func truncateForError(raw []byte) string {
	const max = 512
	s := strings.TrimSpace(string(raw))
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
