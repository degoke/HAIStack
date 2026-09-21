package benchmarks

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// Adapter is the portable runner surface. HAIStack implements it in-process.
// External servers (HAPI, Firely, …) should wrap their HTTP APIs.
type Adapter interface {
	Name() string
	Load(ctx context.Context, resources []*types.ResourceEnvelope) error
	Read(ctx context.Context, resourceType, id string) error
	RunView(ctx context.Context, name, version string) (rows int, err error)
	RunAIView(ctx context.Context, name, version string) (rows int, err error)
}
