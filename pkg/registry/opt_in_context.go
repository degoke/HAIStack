package registry

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
)

type terminologyInstallsContextKey struct{}

// ContextWithTerminologyInstalls overrides the terminology opt-in store for one call tree.
func ContextWithTerminologyInstalls(ctx context.Context, installs store.TerminologyInstallStore) context.Context {
	if installs == nil {
		return ctx
	}
	return context.WithValue(ctx, terminologyInstallsContextKey{}, installs)
}

// TerminologyInstallsFromContext returns a per-request terminology opt-in store when set.
func TerminologyInstallsFromContext(ctx context.Context) store.TerminologyInstallStore {
	if ctx == nil {
		return nil
	}
	installs, _ := ctx.Value(terminologyInstallsContextKey{}).(store.TerminologyInstallStore)
	return installs
}

// ContextWithJobTerminologyInstalls opts the job owner's tenant into terminology installs.
// When the job has no tenant owner, defaultTenantID is used.
func ContextWithJobTerminologyInstalls(ctx context.Context, job store.JobRecord, factory store.TerminologyInstallStoreFactory, defaultTenantID string) (context.Context, error) {
	if factory == nil {
		return ctx, nil
	}
	tenantID := defaultTenantID
	if owner, ok := jobs.OwnerFromPayload(job.Payload); ok && owner.TenantID != "" {
		tenantID = owner.TenantID
	}
	if tenantID == "" {
		return ctx, nil
	}
	installs, err := factory.ForTenant(ctx, tenantID)
	if err != nil {
		return ctx, err
	}
	return ContextWithTerminologyInstalls(ctx, installs), nil
}
