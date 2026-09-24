package ai

import (
	"fmt"

	"github.com/pkoukk/tiktoken-go"
)

// TiktokenEncoding names common OpenAI-compatible encodings (see tiktoken-go).
const (
	TiktokenCl100kBase = "cl100k_base" // GPT-4 / GPT-3.5
	TiktokenO200kBase  = "o200k_base"  // GPT-4o family
)

// NewTiktokenContextCounter returns a ContextTokenCounter backed by tiktoken.
// Use encoding names such as TiktokenCl100kBase or TiktokenO200kBase for OpenAI-compatible models.
func NewTiktokenContextCounter(encoding string) (ContextTokenCounter, error) {
	enc, err := tiktoken.GetEncoding(encoding)
	if err != nil {
		return nil, fmt.Errorf("ai: tiktoken encoding %q: %w", encoding, err)
	}
	return func(messages []ChatMessage) int {
		n := 0
		for _, m := range messages {
			n += len(enc.Encode(m.Content, nil, nil))
			for _, tc := range m.ToolCalls {
				n += len(enc.Encode(tc.ID, nil, nil))
				n += len(enc.Encode(tc.Name, nil, nil))
				n += len(enc.Encode(tc.Arguments, nil, nil))
			}
			n += 4
		}
		return n
	}, nil
}
