package sync

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// SearchIndexer builds search index entries for one resource envelope.
type SearchIndexer interface {
	BuildSearchEntries(ctx context.Context, res *types.ResourceEnvelope) ([]store.SearchIndexEntry, error)
}
