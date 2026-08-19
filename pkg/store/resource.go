package store

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// ResourceStore persists the current state of FHIR resources.
// Delete removes the current record; historical tombstones are recorded through HistoryStore.
type ResourceStore interface {
	Create(ctx context.Context, res *types.ResourceEnvelope) error
	Read(ctx context.Context, resourceType, id string) (*types.ResourceEnvelope, error)
	Update(ctx context.Context, res *types.ResourceEnvelope) error
	Delete(ctx context.Context, resourceType, id string) error
	Exists(ctx context.Context, resourceType, id string) (bool, error)
	ListIDs(ctx context.Context, resourceType string, limit, offset int) ([]string, error)
}

// ConditionalResourceStore adds compare-and-write/delete operations. The
// comparison must happen in the same database statement/transaction as the
// mutation; a read followed by an ordinary Update is not equivalent.
type ConditionalResourceStore interface {
	ResourceStore
	UpdateIfVersion(ctx context.Context, res *types.ResourceEnvelope, expectedVersion string) (bool, error)
	DeleteIfVersion(ctx context.Context, resourceType, id, expectedVersion string) (bool, error)
}
