package ai

import (
	"encoding/json"
	"strings"

	"github.com/degoke/haistack/pkg/store"
)

// invocationToolGroundingContext rebuilds tool summaries and inputs from persisted session events
// so idempotent invocation replay still runs grounding checks.
func invocationToolGroundingContext(events []store.SessionEvent, invocationID string, toolContextFormat ToolContextFormat) ([]HarnessToolResult, []map[string]any) {
	inv := invocationEvents(events, invocationID)
	if len(inv) == 0 {
		return nil, nil
	}
	toolContent := map[string]string{}
	for _, ev := range inv {
		if ev.Author == store.SessionAuthorTool && ev.ToolCallID != "" {
			toolContent[ev.ToolCallID] = ev.Content
		}
	}
	var summaries []HarnessToolResult
	var inputs []map[string]any
	for _, ev := range inv {
		if ev.Author != store.SessionAuthorModel {
			continue
		}
		for _, tc := range ev.ToolCalls {
			input, err := ParseToolArguments(tc.Arguments)
			if err != nil {
				input = map[string]any{}
			}
			inputs = append(inputs, input)
			summary := HarnessToolResult{ToolName: tc.Name}
			content := toolContent[tc.ID]
			if strings.TrimSpace(content) == "" {
				summaries = append(summaries, summary)
				continue
			}
			if isToolErrorContent(content) {
				summary.Err = parseToolErrorContent(content)
				summaries = append(summaries, summary)
				continue
			}
			summary.Result = toolResultFromPersistedContent(tc.Name, content, toolContextFormat)
			summaries = append(summaries, summary)
		}
	}
	return summaries, inputs
}

func isToolErrorContent(content string) bool {
	content = strings.TrimSpace(content)
	return strings.Contains(content, `"error"`)
}

func parseToolErrorContent(content string) error {
	var m map[string]string
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		return ErrInvalidInput
	}
	if msg := m["error"]; msg != "" {
		return &groundingToolReplayError{msg: msg}
	}
	return ErrInvalidInput
}

type groundingToolReplayError struct{ msg string }

func (e *groundingToolReplayError) Error() string { return e.msg }

func toolResultFromPersistedContent(toolName, content string, format ToolContextFormat) *ToolResult {
	content = strings.TrimSpace(content)
	res := &ToolResult{ToolName: toolName}
	if format == ToolContextMarkdown {
		res.Context = content
		var data map[string]any
		if json.Unmarshal([]byte(content), &data) == nil {
			res.Data = data
		}
		return res
	}
	var data any
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		res.Context = content
		return res
	}
	res.Data = data
	res.Context = content
	return res
}
