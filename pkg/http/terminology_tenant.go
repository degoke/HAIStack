package http

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/store"
)

func (h *handler) withTerminologyInstalls(ctx context.Context) (context.Context, error) {
	if h.cfg.TerminologyInstallFactory == nil {
		return ctx, nil
	}
	tenantID := h.cfg.DefaultTerminologyTenantID
	if tenant, ok := h.tenantFromContext(ctx); ok && tenant.TenantID != "" {
		tenantID = tenant.TenantID
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
