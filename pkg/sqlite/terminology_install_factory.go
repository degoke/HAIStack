package sqlite

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// TerminologyInstallStoreFactory resolves SQLite terminology opt-in stores per tenant.
type TerminologyInstallStoreFactory struct {
	db *DB
}

// NewTerminologyInstallStoreFactory returns a factory backed by db.
func NewTerminologyInstallStoreFactory(db *DB) *TerminologyInstallStoreFactory {
	return &TerminologyInstallStoreFactory{db: db}
}

// ForTenant returns the terminology install store for tenantID.
func (f *TerminologyInstallStoreFactory) ForTenant(_ context.Context, tenantID string) (store.TerminologyInstallStore, error) {
	if f == nil || f.db == nil {
		return nil, nil
	}
	return f.db.TerminologyInstallStore(tenantID), nil
}
