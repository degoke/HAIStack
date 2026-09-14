package http

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/store"
)

func (h *handler) withTerminologyContext(ctx context.Context) (context.Context, error) {
	tenantID := h.resolveTerminologyTenantID(ctx)
	if tenantID != "" {
		ctx = store.ContextWithTerminologyScope(ctx, tenantID)
	}
	if h.cfg.TerminologyInstallFactory == nil {
		return ctx, nil
	}
	if tenantID == "" {
		tenantID = h.cfg.DefaultTerminologyTenantID
	}
	if tenantID == "" {
		return ctx, nil
	}
	installs, err := h.cfg.TerminologyInstallFactory.ForTenant(ctx, tenantID)
	if err != nil {
		return ctx, err
	}
	return store.ContextWithTerminologyInstalls(ctx, installs), nil
}

func (h *handler) resolveTerminologyTenantID(ctx context.Context) string {
	if tenant, ok := h.tenantFromContext(ctx); ok && tenant.TenantID != "" {
		return tenant.TenantID
	}
	return ""
}

func (h *handler) terminologyScope(ctx context.Context) string {
	if scope := store.TerminologyScopeFromContext(ctx); scope != "" {
		return scope
	}
	if tenantID := h.resolveTerminologyTenantID(ctx); tenantID != "" {
		return tenantID
	}
	return h.cfg.TerminologyScope
}

// withTerminologyInstalls attaches tenant opt-in and overlay scope for terminology calls.
func (h *handler) withTerminologyInstalls(ctx context.Context) (context.Context, error) {
	return h.withTerminologyContext(ctx)
}
