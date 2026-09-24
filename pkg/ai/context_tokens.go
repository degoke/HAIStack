package ai

import "encoding/json"

// ContextTokenCounter estimates token usage for a model message list (for compaction thresholds).
type ContextTokenCounter func(messages []ChatMessage) int

// EstimateChatMessagesTokens is a coarse token estimate (~utf-8 bytes/4 plus per-message overhead).
// Prefer NewTiktokenContextCounter for OpenAI-compatible models when accuracy matters.
func EstimateChatMessagesTokens(messages []ChatMessage) int {
	n := 0
	for _, m := range messages {
		n += 4 // role / framing overhead
		n += estimateTokenCount(m.Content)
		for _, tc := range m.ToolCalls {
			n += estimateTokenCount(tc.ID) + estimateTokenCount(tc.Name) + estimateTokenCount(tc.Arguments)
		}
	}
	return n
}

func estimateTokenCount(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

func estimateChatToolsTokens(tools []ChatTool, counter ContextTokenCounter) int {
	if len(tools) == 0 || counter == nil {
		return 0
	}
	raw, err := json.Marshal(tools)
	if err != nil {
		return 0
	}
	return counter([]ChatMessage{{Role: ChatRoleSystem, Content: string(raw)}})
}
