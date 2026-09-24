package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ToolCallProtocol selects how the harness interprets model tool requests.
type ToolCallProtocol string

const (
	// ToolCallProtocolNative uses provider-native tool_calls only (OpenAI function tools).
	ToolCallProtocolNative ToolCallProtocol = "native"
	// ToolCallProtocolPromptJSON parses tool JSON from assistant message text.
	ToolCallProtocolPromptJSON ToolCallProtocol = "prompt_json"
	// ToolCallProtocolBoth accepts native tool_calls first, then prompt JSON fallback.
	ToolCallProtocolBoth ToolCallProtocol = "both"
)

// PromptToolInvocation is one JSON tool call embedded in model text.
type PromptToolInvocation struct {
	Tool      string         `json:"tool"`
	ToolName  string         `json:"toolName"`
	Input     map[string]any `json:"input"`
	Arguments map[string]any `json:"arguments"`
}

// AppendPromptJSONToolInstructions extends a system prompt with tool-calling format guidance.
func AppendPromptJSONToolInstructions(systemPrompt string, tools []ChatTool) string {
	var b strings.Builder
	if strings.TrimSpace(systemPrompt) != "" {
		b.WriteString(strings.TrimSpace(systemPrompt))
		b.WriteString("\n\n")
	}
	b.WriteString("When you need to call a tool, emit a single JSON object on its own line ")
	b.WriteString("(or inside a ```json fenced block) using this shape:\n")
	b.WriteString(`{"tool":"<name>","input":{...}}` + "\n")
	b.WriteString("Available tools:\n")
	for _, tool := range tools {
		b.WriteString("- ")
		b.WriteString(tool.Name)
		if tool.Description != "" {
			b.WriteString(": ")
			b.WriteString(tool.Description)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// ParsePromptToolCalls extracts tool invocations from assistant text.
func ParsePromptToolCalls(content string, allowed []ChatTool) ([]ChatToolCall, error) {
	allowedNames := map[string]struct{}{}
	for _, t := range allowed {
		allowedNames[t.Name] = struct{}{}
	}
	var invocations []PromptToolInvocation
	if block := extractFencedBlock(content, "json"); block != "" {
		invocations = decodePromptInvocations(block)
	}
	if len(invocations) == 0 {
		invocations = scanPromptInvocations(content)
	}
	if len(invocations) == 0 {
		return nil, nil
	}
	out := make([]ChatToolCall, 0, len(invocations))
	for _, inv := range invocations {
		name := strings.TrimSpace(inv.Tool)
		if name == "" {
			name = strings.TrimSpace(inv.ToolName)
		}
		if name == "" {
			continue
		}
		if len(allowedNames) > 0 {
			if _, ok := allowedNames[name]; !ok {
				return nil, fmt.Errorf("ai: prompt tool %q is not in the allowed tool list", name)
			}
		}
		input := inv.Input
		if input == nil {
			input = inv.Arguments
		}
		if input == nil {
			input = map[string]any{}
		}
		args, err := json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("ai: prompt tool %q input: %w", name, err)
		}
		out = append(out, ChatToolCall{
			ID:        "prompt-" + uuid.NewString(),
			Name:      name,
			Arguments: string(args),
		})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func decodePromptInvocations(raw string) []PromptToolInvocation {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if inv, ok := decodeOnePromptInvocation(raw); ok {
		return []PromptToolInvocation{inv}
	}
	var list []PromptToolInvocation
	if err := json.Unmarshal([]byte(raw), &list); err == nil && len(list) > 0 {
		return list
	}
	var envelope struct {
		Tools []PromptToolInvocation `json:"tools"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err == nil && len(envelope.Tools) > 0 {
		return envelope.Tools
	}
	return nil
}

func scanPromptInvocations(content string) []PromptToolInvocation {
	var out []PromptToolInvocation
	for i := 0; i < len(content); i++ {
		if content[i] != '{' {
			continue
		}
		end := jsonObjectEnd(content, i)
		if end < 0 {
			continue
		}
		chunk := content[i : end+1]
		if inv, ok := decodeOnePromptInvocation(chunk); ok {
			out = append(out, inv)
		}
	}
	return out
}

func decodeOnePromptInvocation(raw string) (PromptToolInvocation, bool) {
	var inv PromptToolInvocation
	if err := json.Unmarshal([]byte(raw), &inv); err != nil {
		return PromptToolInvocation{}, false
	}
	name := strings.TrimSpace(inv.Tool)
	if name == "" {
		name = strings.TrimSpace(inv.ToolName)
	}
	if name == "" {
		return PromptToolInvocation{}, false
	}
	if inv.Input == nil && inv.Arguments == nil {
		return PromptToolInvocation{}, false
	}
	return inv, true
}

func jsonObjectEnd(s string, start int) int {
	if start >= len(s) || s[start] != '{' {
		return -1
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
