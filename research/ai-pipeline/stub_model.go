package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/ai"
)

// StubModel is a deterministic, offline language-model stand-in.
// It does not call any network API. The seed is mixed into the digest so
// runs with the same context always produce the same content.
type StubModel struct {
	Adapter string
	Seed    int64
}

func (m *StubModel) Name() string {
	if m == nil || m.Adapter == "" {
		return "stub-v1"
	}
	return m.Adapter
}

func (m *StubModel) Invoke(_ context.Context, req ai.ModelRequest) (*ai.ModelResponse, error) {
	seed := int64(11)
	if m != nil && m.Seed != 0 {
		seed = m.Seed
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\n%s\n%s", seed, req.Prompt, req.Context)))
	digest := hex.EncodeToString(sum[:8])
	var b strings.Builder
	b.WriteString("Deterministic vitals summary (stub model, seed ")
	b.WriteString(fmt.Sprintf("%d). Fingerprint %s.\n", seed, digest))
	if strings.TrimSpace(req.Context) != "" {
		b.WriteString("Grounded on permissioned view rows supplied in tool context.")
	}
	return &ai.ModelResponse{Adapter: m.Name(), Content: b.String()}, nil
}
