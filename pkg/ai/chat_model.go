package ai

import (
	"context"
	"encoding/json"
)

// Chat roles follow common OpenAI-compatible chat APIs.
const (
	ChatRoleSystem    = "system"
	ChatRoleUser      = "user"
	ChatRoleAssistant = "assistant"
	ChatRoleTool      = "tool"
)

// ChatMessage is one turn in a conversational model session.
type ChatMessage struct {
	Role       string
	Content    string
	ToolCallID string
	ToolCalls  []ChatToolCall
}

// ChatToolCall is a model-requested tool invocation (function-style).
type ChatToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// ChatTool describes one callable tool for the model (JSON-schema parameters).
type ChatTool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// ChatRequest is input to a conversational model backend.
type ChatRequest struct {
	Messages     []ChatMessage
	Tools        []ChatTool
	SystemPrompt string
	Hint         string
}

// ChatResponse is the outcome of one model completion.
type ChatResponse struct {
	Adapter    string
	Content    string
	ToolCalls  []ChatToolCall
	StopReason string
}

// ChatModel performs multi-message completions with optional tool definitions.
// Implementations include OpenAICompatibleAdapter and test fakes.
type ChatModel interface {
	Name() string
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
}

// ChatToolsFromDescriptors builds OpenAI-style function tools from registry metadata.
func ChatToolsFromDescriptors(descriptors []ToolDescriptor) []ChatTool {
	out := make([]ChatTool, 0, len(descriptors))
	for _, d := range descriptors {
		props := map[string]any{}
		for _, key := range d.InputKeys {
			props[key] = map[string]any{"type": "string"}
		}
		params := map[string]any{
			"type":       "object",
			"properties": props,
		}
		if len(props) > 0 {
			params["additionalProperties"] = true
		}
		out = append(out, ChatTool{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  params,
		})
	}
	return out
}

// ParseToolArguments decodes a tool call arguments JSON object into a map.
func ParseToolArguments(arguments string) (map[string]any, error) {
	arguments = trimSpace(arguments)
	if arguments == "" {
		return map[string]any{}, nil
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return nil, err
	}
	if input == nil {
		return map[string]any{}, nil
	}
	return input, nil
}

func trimSpace(s string) string {
	i := 0
	j := len(s)
	for i < j && (s[i] == ' ' || s[i] == '\n' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\n' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}
