package ai

import (
	"context"
	"fmt"
	"strings"
)

// ModelAdapterChatModel wraps a legacy ModelAdapter as a ChatModel for Harness.
// The adapter receives a flattened transcript and does not emit tool calls; use
// OpenAICompatibleAdapter (or another native ChatModel) for tool-calling loops.
type ModelAdapterChatModel struct {
	Adapter ModelAdapter
	Hint    string
}

// NewModelAdapterChatModel returns a ChatModel backed by ModelAdapter.Invoke.
func NewModelAdapterChatModel(adapter ModelAdapter, hint string) (*ModelAdapterChatModel, error) {
	if adapter == nil {
		return nil, fmt.Errorf("ai: model adapter chat bridge requires adapter")
	}
	return &ModelAdapterChatModel{Adapter: adapter, Hint: hint}, nil
}

// Name implements ChatModel.
func (m *ModelAdapterChatModel) Name() string {
	if m == nil || m.Adapter == nil {
		return ""
	}
	return m.Adapter.Name()
}

// Chat implements ChatModel by delegating to ModelAdapter.Invoke.
func (m *ModelAdapterChatModel) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if m == nil || m.Adapter == nil {
		return nil, fmt.Errorf("ai: nil model adapter chat bridge")
	}
	prompt, contextText := flattenChatTranscript(req.Messages)
	if sp := strings.TrimSpace(req.SystemPrompt); sp != "" {
		contextText = strings.TrimSpace(contextText)
		if contextText != "" {
			contextText = "System:\n" + sp + "\n\n" + contextText
		} else {
			contextText = sp
		}
	}
	hint := m.Hint
	if hint == "" {
		hint = req.Hint
	}
	resp, err := m.Adapter.Invoke(ctx, ModelRequest{
		Hint:    hint,
		Prompt:  prompt,
		Context: contextText,
		Tools:   toolNamesFromChatTools(req.Tools),
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &ChatResponse{Adapter: m.Name()}, nil
	}
	return &ChatResponse{
		Adapter: resp.Adapter,
		Content: resp.Content,
	}, nil
}

func flattenChatTranscript(messages []ChatMessage) (prompt string, context string) {
	var ctxLines []string
	var lastUser string
	for _, m := range messages {
		switch m.Role {
		case ChatRoleUser:
			lastUser = m.Content
			ctxLines = append(ctxLines, "User: "+m.Content)
		case ChatRoleAssistant:
			line := "Assistant: " + m.Content
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					line += fmt.Sprintf(" [tool_call %s %s]", tc.Name, tc.Arguments)
				}
			}
			ctxLines = append(ctxLines, line)
		case ChatRoleTool:
			ctxLines = append(ctxLines, "Tool("+m.ToolCallID+"): "+m.Content)
		case ChatRoleSystem:
			ctxLines = append(ctxLines, "System: "+m.Content)
		}
	}
	if lastUser != "" {
		prompt = lastUser
	}
	return prompt, strings.Join(ctxLines, "\n")
}

func toolNamesFromChatTools(tools []ChatTool) []string {
	if len(tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}
