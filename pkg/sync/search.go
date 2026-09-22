package sync

import (
	"context"

	"github.com/degoke/haistack/pkg/store"
	"github.com/degoke/haistack/pkg/types"
)

// SearchIndexer builds search index entries for one resource envelope.
type SearchIndexer interface {
	BuildSearchEntries(ctx context.Context, res *types.ResourceEnvelope) ([]store.SearchIndexEntry, error)
}
