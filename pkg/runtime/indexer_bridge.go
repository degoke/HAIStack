package runtime

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// indexerSyncBridge adapts search.Indexer to sync.SearchIndexer.
type indexerSyncBridge struct {
	indexer search.Indexer
}

func (b *indexerSyncBridge) BuildSearchEntries(ctx context.Context, res *types.ResourceEnvelope) ([]store.SearchIndexEntry, error) {
	if b == nil || b.indexer == nil {
		return nil, nil
	}
	return b.indexer.Build(ctx, res)
}
