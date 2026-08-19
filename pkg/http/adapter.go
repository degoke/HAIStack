package http

import (
	"context"
	"net/url"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// CoreResourceService adapts *core.ResourceService to ResourceService.
type CoreResourceService struct {
	Svc *core.ResourceService
}

func (a CoreResourceService) Create(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	return a.Svc.Create(ctx, resource)
}

func (a CoreResourceService) Read(ctx context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	return a.Svc.Read(ctx, resourceType, id)
}

func (a CoreResourceService) Update(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	return a.Svc.Update(ctx, resource)
}

func (a CoreResourceService) Delete(ctx context.Context, resourceType, id string) error {
	return a.Svc.Delete(ctx, resourceType, id)
}

func (a CoreResourceService) History(ctx context.Context, resourceType, id string) ([]store.ResourceVersion, error) {
	return a.Svc.History(ctx, resourceType, id)
}

func (a CoreResourceService) ProcessTransactionBundle(ctx context.Context, bundle *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	return a.Svc.ProcessTransactionBundle(ctx, bundle)
}

func (a CoreResourceService) ProcessBatchBundle(ctx context.Context, bundle *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	return a.Svc.ProcessBatchBundle(ctx, bundle)
}

func (a CoreResourceService) Patch(ctx context.Context, resourceType, id string, patchJSON []byte) (*types.ResourceEnvelope, error) {
	return a.Svc.Patch(ctx, resourceType, id, patchJSON)
}

func (a CoreResourceService) UpdateIfMatch(ctx context.Context, resource *types.ResourceEnvelope, expectedVersion string) (*types.ResourceEnvelope, error) {
	return a.Svc.UpdateIfMatch(ctx, resource, expectedVersion)
}

func (a CoreResourceService) DeleteIfMatch(ctx context.Context, resourceType, id, expectedVersion string) error {
	return a.Svc.DeleteIfMatch(ctx, resourceType, id, expectedVersion)
}

func (a CoreResourceService) PatchIfMatch(ctx context.Context, resourceType, id string, patchJSON []byte, expectedVersion string) (*types.ResourceEnvelope, error) {
	return a.Svc.PatchIfMatch(ctx, resourceType, id, patchJSON, expectedVersion)
}

type searchBundleExecutor interface {
	SearchBundle(ctx context.Context, resourceType string, params url.Values) (*search.SearchBundle, error)
}

// SearchServiceAdapter adapts *search.Service to SearchService and
// PatientScopedSearchService. PatientSearchParamResolver derives scope
// parameters from installed registry SearchParameters when configured.
type SearchServiceAdapter struct {
	Svc                       searchBundleExecutor
	PatientSearchParamResolver auth.PatientSearchParamResolver
}

func (a SearchServiceAdapter) SearchBundle(ctx context.Context, resourceType string, params url.Values) (*search.SearchBundle, error) {
	return a.Svc.SearchBundle(ctx, resourceType, params)
}

// SearchBundleForPatient injects a patient relationship filter into search params
// before executing the query so unauthorized rows are excluded at query time.
func (a SearchServiceAdapter) SearchBundleForPatient(ctx context.Context, resourceType, patientID string, params url.Values) (*search.SearchBundle, error) {
	scoped, err := auth.ApplyPatientSearchScopeToParams(params, resourceType, patientID, a.PatientSearchParamResolver)
	if err != nil {
		return nil, err
	}
	return a.Svc.SearchBundle(ctx, resourceType, scoped)
}

// RegistryCapabilitySource adapts *registry.Snapshot to CapabilitySource.
type RegistryCapabilitySource struct {
	Snapshot *registry.Snapshot
}

func (s RegistryCapabilitySource) CapabilitySnapshot() registry.CapabilitySnapshot {
	return s.Snapshot.CapabilitySnapshot()
}

// PolicyAuthChecker adapts auth.PolicyEngine to AuthChecker.
type PolicyAuthChecker struct {
	Engine auth.PolicyEngine
}

func (c PolicyAuthChecker) AuthorizeRead(ctx context.Context, principal auth.Principal, tenant auth.TenantContext, resourceType, id string) (auth.Decision, error) {
	return c.Engine.CanReadResource(ctx, auth.ReadRequest{
		Principal:    principal,
		Tenant:       tenant,
		ResourceType: resourceType,
		ID:           id,
	})
}

func (c PolicyAuthChecker) AuthorizeWrite(ctx context.Context, principal auth.Principal, tenant auth.TenantContext, operation, resourceType, id string) (auth.Decision, error) {
	return c.Engine.CanWriteResource(ctx, auth.WriteRequest{
		Principal:    principal,
		Tenant:       tenant,
		Operation:    operation,
		ResourceType: resourceType,
		ID:           id,
	})
}

func (c PolicyAuthChecker) AuthorizeSearch(ctx context.Context, principal auth.Principal, tenant auth.TenantContext, resourceType string) (auth.Decision, error) {
	return c.Engine.CanReadResource(ctx, auth.ReadRequest{
		Principal:    principal,
		Tenant:       tenant,
		ResourceType: resourceType,
	})
}
