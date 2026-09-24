package ai

// ContextTokenCounter estimates token usage for a model message list (for compaction thresholds).
type ContextTokenCounter func(messages []ChatMessage) int

// EstimateChatMessagesTokens is a coarse token estimate (~utf-8 bytes/4 plus per-message overhead).
// Replace with a model-specific counter via SessionCompaction.TokenCounter when you need tighter bounds.
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
	// Ceil(len/4) — common heuristic when a tokenizer is not wired.
	return (len(s) + 3) / 4
}
