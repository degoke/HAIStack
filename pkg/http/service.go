package http

import (
	"context"
	"net/url"

	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// ResourceService is the narrow resource lifecycle surface the HTTP adapter needs.
type ResourceService interface {
	Create(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error)
	Read(ctx context.Context, resourceType, id string) (*types.ResourceEnvelope, error)
	Update(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error)
	Delete(ctx context.Context, resourceType, id string) error
	History(ctx context.Context, resourceType, id string) ([]store.ResourceVersion, error)
	ProcessTransactionBundle(ctx context.Context, bundle *types.ResourceEnvelope) (*types.ResourceEnvelope, error)
}

// SearchService is the narrow search surface the HTTP adapter needs.
type SearchService interface {
	SearchBundle(ctx context.Context, resourceType string, params url.Values) (*search.SearchBundle, error)
}

// CapabilitySource returns the compiled registry capability snapshot for metadata.
type CapabilitySource interface {
	CapabilitySnapshot() registry.CapabilitySnapshot
}
