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
	ProcessBatchBundle(ctx context.Context, bundle *types.ResourceEnvelope) (*types.ResourceEnvelope, error)
	Patch(ctx context.Context, resourceType, id string, patchJSON []byte) (*types.ResourceEnvelope, error)
}

// ConditionalResourceService is implemented by services that can enforce
// If-Match atomically with the write. ResourceService remains intentionally
// small for adapters and test doubles that do not provide this extension.
type ConditionalResourceService interface {
	UpdateIfMatch(ctx context.Context, resource *types.ResourceEnvelope, expectedVersion string) (*types.ResourceEnvelope, error)
	DeleteIfMatch(ctx context.Context, resourceType, id, expectedVersion string) error
	PatchIfMatch(ctx context.Context, resourceType, id string, patchJSON []byte, expectedVersion string) (*types.ResourceEnvelope, error)
}

// SearchService is the narrow search surface the HTTP adapter needs.
type SearchService interface {
	SearchBundle(ctx context.Context, resourceType string, params url.Values) (*search.SearchBundle, error)
}

// PatientScopedSearchService is required when the authenticated tenant is
// restricted to one patient. A regular SearchService cannot safely infer a
// patient filter from an auth decision, so the implementation must apply the
// restriction while planning/executing the search.
type PatientScopedSearchService interface {
	SearchBundleForPatient(ctx context.Context, resourceType, patientID string, params url.Values) (*search.SearchBundle, error)
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

// OperationRequest is the generic FHIR operation boundary. Custom operation
// implementations own their Parameters validation and response type.
type OperationRequest struct {
	ResourceType string
	ID           string
	Operation    string
	Query        url.Values
	Body         []byte
}

type OperationService interface {
	Execute(context.Context, OperationRequest) (*types.ResourceEnvelope, error)
}

// ValidateRequest carries FHIR Resource/$validate inputs.
type ValidateRequest struct {
	ResourceType string
	ID           string
	ContentType  string
	Body         []byte
	Query        url.Values
}

// ValidateService runs FHIR Resource/$validate and returns an OperationOutcome.
type ValidateService interface {
	Validate(context.Context, ValidateRequest) (*types.OperationOutcome, error)
}
