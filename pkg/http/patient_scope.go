package http

import (
	"context"
	"errors"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func (h *handler) tenantFromContext(ctx context.Context) (auth.TenantContext, bool) {
	_, tenant, ok := identityFromContext(ctx)
	return tenant, ok
}

func (h *handler) enforcePatientScopeOnEnvelope(ctx context.Context, envelope *types.ResourceEnvelope) error {
	if h.cfg.PatientReferenceResolver == nil {
		return nil
	}
	tenant, ok := h.tenantFromContext(ctx)
	if !ok || tenant.PatientScope == "" {
		return nil
	}
	return auth.CheckEnvelopePatientScope(ctx, tenant, h.cfg.PatientReferenceResolver, envelope)
}

func (h *handler) filterSearchBundlePatientScope(ctx context.Context, bundle *search.SearchBundle) error {
	if bundle == nil || h.cfg.PatientReferenceResolver == nil {
		return nil
	}
	tenant, ok := h.tenantFromContext(ctx)
	if !ok || tenant.PatientScope == "" {
		return nil
	}
	kept := make([]search.BundleEntry, 0, len(bundle.Entries))
	for _, entry := range bundle.Entries {
		if entry.Resource == nil {
			continue
		}
		if err := auth.CheckEnvelopePatientScope(ctx, tenant, h.cfg.PatientReferenceResolver, entry.Resource); err != nil {
			if errors.Is(err, auth.ErrDenied) {
				continue
			}
			return err
		}
		kept = append(kept, entry)
	}
	bundle.Entries = kept
	bundle.Count = len(kept)
	if bundle.Total != nil {
		total := len(kept)
		bundle.Total = &total
	}
	return nil
}
