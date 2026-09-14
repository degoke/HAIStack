package store

import "context"

// TerminologyInstallStoreFactory resolves per-tenant terminology opt-in stores.
type TerminologyInstallStoreFactory interface {
	ForTenant(ctx context.Context, tenantID string) (TerminologyInstallStore, error)
}
