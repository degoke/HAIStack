package aipipeline

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/ai"
)

// SeededStub is a deterministic ModelAdapter. It never calls a network LLM.
type SeededStub struct {
	Seed int64
}

// Name implements ai.ModelAdapter.
func (s SeededStub) Name() string { return "seeded-stub" }

// Invoke implements ai.ModelAdapter. The content is a hex prefix of
// SHA-256(seed, prompt, context) so reruns match without API keys.
func (s SeededStub) Invoke(_ context.Context, req ai.ModelRequest) (*ai.ModelResponse, error) {
	seed := s.Seed
	if seed == 0 {
		seed = StubSeed
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("seed=%d\nprompt=%s\ncontext=%s", seed, req.Prompt, req.Context)))
	return &ai.ModelResponse{
		Adapter: s.Name(),
		Content: fmt.Sprintf("lab-summary:%x", sum[:12]),
	}, nil
}

// Prompt is the fixed instruction used with the stub.
const Prompt = "Summarize the permissioned lab observation rows for reproducible clinical AI research. Do not invent patients."
