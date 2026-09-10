package http

import (
	"context"
	"errors"
	"net/url"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/smart"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func (h *handler) scopeBundleFromContext(ctx context.Context) (smart.ScopeSet, smart.ActorClass, bool) {
	bundle, ok := smart.AuthBundleFromContext(ctx)
	if !ok || bundle.Scopes.Empty() {
		return smart.ScopeSet{}, "", false
	}
	actor := smart.ActorForPrincipal(bundle.Principal.Kind, bundle.Scopes)
	if actor == "" {
		return smart.ScopeSet{}, "", false
	}
	return bundle.Scopes, actor, true
}

func (h *handler) applyScopeFiltersToSearchParams(ctx context.Context, resourceType string, params url.Values) (url.Values, error) {
	scopes, actor, ok := h.scopeBundleFromContext(ctx)
	if !ok {
		return params, nil
	}
	return smart.ApplyScopeFiltersToParams(scopes, actor, resourceType, params)
}

func (h *handler) enforceScopeFiltersOnEnvelope(ctx context.Context, resourceType string, op smart.AccessOp, envelope *types.ResourceEnvelope) error {
	scopes, actor, ok := h.scopeBundleFromContext(ctx)
	if !ok {
		return nil
	}
	return smart.CheckEnvelopeScopeFilters(scopes, actor, resourceType, op, envelope)
}

func (h *handler) filterSearchBundleScopeFilters(ctx context.Context, resourceType string, bundle *search.SearchBundle) error {
	scopes, actor, ok := h.scopeBundleFromContext(ctx)
	if ok {
		if err := smart.FilterSearchBundleScopeFilters(scopes, actor, resourceType, bundle); err != nil {
			return err
		}
	}
	return h.filterSearchBundlePatientScope(ctx, bundle)
}

func scopeFilterError(err error) error {
	if errors.Is(err, smart.ErrScopeFilterDenied) {
		return err
	}
	return err
}

func (h *handler) enforceWriteScopeFilters(ctx context.Context, resourceType string, op smart.AccessOp, envelope *types.ResourceEnvelope) error {
	if !h.needsResourceScopeEnforcement(ctx) {
		return nil
	}
	if err := h.enforcePatientScopeOnEnvelope(ctx, envelope); err != nil {
		return err
	}
	return h.enforceScopeFiltersOnEnvelope(ctx, resourceType, op, envelope)
}

func (h *handler) needsResourceScopeEnforcement(ctx context.Context) bool {
	if _, _, ok := h.scopeBundleFromContext(ctx); ok {
		return true
	}
	tenant, ok := h.tenantFromContext(ctx)
	return ok && tenant.PatientScope != "" && h.cfg.PatientReferenceResolver != nil
}

func (h *handler) enforceDeleteScopeFilters(ctx context.Context, resourceType, id string, op smart.AccessOp) error {
	if h.cfg.ResourceService == nil || !h.needsResourceScopeEnforcement(ctx) {
		return nil
	}
	envelope, err := h.cfg.ResourceService.Read(ctx, resourceType, id)
	if err != nil {
		return err
	}
	return h.enforceWriteScopeFilters(ctx, resourceType, op, envelope)
}

func (h *handler) filterOperationBundleResult(ctx context.Context, defaultResourceType string, envelope *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	scopes, actor, ok := h.scopeBundleFromContext(ctx)
	if !ok || envelope == nil || envelope.ResourceType != "Bundle" {
		return envelope, nil
	}
	return smart.FilterBundleEnvelopeScopeFilters(scopes, actor, defaultResourceType, envelope, h.cfg.Codec)
}

func (h *handler) filterHistoryVersions(ctx context.Context, resourceType string, versions []store.ResourceVersion) ([]store.ResourceVersion, error) {
	scopes, actor, ok := h.scopeBundleFromContext(ctx)
	if !ok {
		return filterHistoryPatientScope(ctx, h, versions)
	}
	filtered := make([]store.ResourceVersion, 0, len(versions))
	for _, version := range versions {
		if version.Resource == nil {
			continue
		}
		if err := h.enforcePatientScopeOnEnvelope(ctx, version.Resource); err != nil {
			if errors.Is(err, auth.ErrDenied) {
				continue
			}
			return nil, err
		}
		if err := smart.CheckEnvelopeScopeFilters(scopes, actor, resourceType, smart.OpRead, version.Resource); err != nil {
			if errors.Is(err, smart.ErrScopeFilterDenied) {
				continue
			}
			return nil, err
		}
		filtered = append(filtered, version)
	}
	return filtered, nil
}

func filterHistoryPatientScope(ctx context.Context, h *handler, versions []store.ResourceVersion) ([]store.ResourceVersion, error) {
	tenant, ok := h.tenantFromContext(ctx)
	if !ok || tenant.PatientScope == "" || h.cfg.PatientReferenceResolver == nil {
		return versions, nil
	}
	filtered := make([]store.ResourceVersion, 0, len(versions))
	for _, version := range versions {
		if version.Resource == nil {
			continue
		}
		if err := auth.CheckEnvelopePatientScope(ctx, tenant, h.cfg.PatientReferenceResolver, version.Resource); err != nil {
			if errors.Is(err, auth.ErrDenied) {
				continue
			}
			return nil, err
		}
		filtered = append(filtered, version)
	}
	return filtered, nil
}
