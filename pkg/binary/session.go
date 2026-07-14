package binary

import "github.com/degoke/health-ai-stack/pkg/store"

// WriteSessionExtension exposes transaction-scoped binary metadata persistence.
// SQLite and Postgres sessions implement this additively without changing
// store.WriteSession.
type WriteSessionExtension interface {
	BinaryMetadataStore() MetadataStore
}

// MetadataFromWriteSession returns a transaction-scoped MetadataStore when the
// write session supports binary metadata extensions.
func MetadataFromWriteSession(ws store.WriteSession) (MetadataStore, bool) {
	ext, ok := ws.(WriteSessionExtension)
	if !ok {
		return nil, false
	}
	return ext.BinaryMetadataStore(), true
}
