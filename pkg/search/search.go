package search

import (
	"context"

	"github.com/degoke/haistack/pkg/store"
	"github.com/degoke/haistack/pkg/types"
)

// Indexer builds search index entries from a resource envelope after writes in pkg/core.
type Indexer interface {
	// Build returns index entries to persist via store.SearchStore.
	Build(ctx context.Context, resource *types.ResourceEnvelope) ([]store.SearchIndexEntry, error)
}
