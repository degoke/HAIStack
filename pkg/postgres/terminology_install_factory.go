package postgres

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// TerminologyInstallStoreFactory resolves Postgres terminology opt-in stores per tenant.
type TerminologyInstallStoreFactory struct {
	db *DB
}

// NewTerminologyInstallStoreFactory returns a factory backed by db.
func NewTerminologyInstallStoreFactory(db *DB) *TerminologyInstallStoreFactory {
	return &TerminologyInstallStoreFactory{db: db}
}

// ForTenant returns the terminology install store for tenantID.
func (f *TerminologyInstallStoreFactory) ForTenant(ctx context.Context, tenantID string) (store.TerminologyInstallStore, error) {
	if f == nil || f.db == nil || tenantID == "" {
		return nil, nil
	}
	if err := f.db.EnsureTenant(ctx, tenantID); err != nil {
		return nil, fmt.Errorf("ensure tenant %q: %w", tenantID, err)
	}
	return f.db.Tenant(tenantID).TerminologyInstallStore(), nil
}
