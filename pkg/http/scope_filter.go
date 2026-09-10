package http

import (
	"context"
	"errors"
	"net/url"

	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/smart"
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
	if !ok {
		return nil
	}
	if err := smart.FilterSearchBundleScopeFilters(scopes, actor, resourceType, bundle); err != nil {
		return err
	}
	if err := h.filterSearchBundlePatientScope(ctx, bundle); err != nil {
		return err
	}
	return nil
}

func scopeFilterError(err error) error {
	if errors.Is(err, smart.ErrScopeFilterDenied) {
		return err
	}
	return err
}
