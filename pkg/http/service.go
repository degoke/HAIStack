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

// SDCRequest carries canonical FHIR operation inputs to the SDC adapter. The
// adapter owns population, validation, assembly, extraction, and adaptive
// policy; HTTP only performs envelope parsing and response translation.
type SDCRequest struct {
	Questionnaire         *types.ResourceEnvelope
	QuestionnaireResponse *types.ResourceEnvelope
	Parameters            *types.ResourceEnvelope
	Body                  *types.ResourceEnvelope
	Query                 url.Values
}

type SDCService interface {
	Populate(context.Context, SDCRequest) (*types.ResourceEnvelope, error)
	Validate(context.Context, SDCRequest) (*types.OperationOutcome, error)
	Extract(context.Context, SDCRequest) (*types.ResourceEnvelope, error)
	Assemble(context.Context, SDCRequest) (*types.ResourceEnvelope, error)
	Adaptive(context.Context, string, SDCRequest) (*types.ResourceEnvelope, error)
}
