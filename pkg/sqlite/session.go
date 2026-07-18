package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/binary"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// Session coordinates transaction-scoped store access for atomic local writes.
type Session struct {
	tx *sql.Tx

	resource    *ResourceStore
	history     *HistoryStore
	search      *SearchStore
	outbox      *OutboxStore
	cursor      *CursorStore
	terminology *TerminologyStore

	committed  bool
	rolledBack bool
}

// BeginSession starts a new SQLite transaction session.
func (db *DB) BeginSession(ctx context.Context) (*Session, error) {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin session: %w", err)
	}
	return newSession(tx), nil
}

func newSession(tx *sql.Tx) *Session {
	return &Session{
		tx:          tx,
		resource:    newResourceStoreTx(tx),
		history:     newHistoryStoreTx(tx),
		search:      newSearchStoreTx(tx),
		outbox:      newOutboxStoreTx(tx),
		cursor:      newCursorStoreTx(tx),
		terminology: newTerminologyStore(tx, "default"),
	}
}

// TerminologyStore returns the transaction-scoped terminology projection store.
func (s *Session) TerminologyStore() store.TerminologyStore { return s.terminology }

// Commit commits the session transaction.
func (s *Session) Commit(ctx context.Context) error {
	_ = ctx
	if s.committed {
		return fmt.Errorf("session already committed")
	}
	if s.rolledBack {
		return fmt.Errorf("session already rolled back")
	}
	if err := s.tx.Commit(); err != nil {
		return fmt.Errorf("commit session: %w", err)
	}
	s.committed = true
	return nil
}

// Rollback rolls back the session transaction.
func (s *Session) Rollback(ctx context.Context) error {
	_ = ctx
	if s.committed {
		return fmt.Errorf("session already committed")
	}
	if s.rolledBack {
		return nil
	}
	if err := s.tx.Rollback(); err != nil {
		return fmt.Errorf("rollback session: %w", err)
	}
	s.rolledBack = true
	return nil
}

// ResourceStore returns the transaction-scoped resource store.
func (s *Session) ResourceStore() store.ResourceStore {
	return s.resource
}

// HistoryStore returns the transaction-scoped history store.
func (s *Session) HistoryStore() store.HistoryStore {
	return s.history
}

// SearchStore returns the transaction-scoped search store.
func (s *Session) SearchStore() store.SearchStore {
	return s.search
}

// OutboxStore returns the transaction-scoped outbox event store.
func (s *Session) OutboxStore() store.EventStore {
	return s.outbox
}

// EventStore returns the transaction-scoped event store.
func (s *Session) EventStore() store.EventStore {
	return s.outbox
}

// CursorStore returns the transaction-scoped cursor store.
func (s *Session) CursorStore() store.CursorStore {
	return s.cursor
}

// BinaryMetadataStore returns the transaction-scoped blob metadata store.
func (s *Session) BinaryMetadataStore() binary.MetadataStore {
	return newBlobMetadataStoreTx(s.tx)
}

func (s *Session) applyLocalWrite(ctx context.Context, input LocalWrite) (*LocalWriteResult, error) {
	switch input.Action {
	case store.VersionActionCreate:
		if err := s.resource.Create(ctx, input.Resource); err != nil {
			return nil, err
		}
	case store.VersionActionUpdate:
		if err := s.resource.Update(ctx, input.Resource); err != nil {
			return nil, err
		}
	case store.VersionActionDelete:
		if err := s.resource.Delete(ctx, input.Resource.ResourceType, input.Resource.ID); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported version action %q", input.Action)
	}

	if err := s.history.AppendVersion(ctx, input.Version); err != nil {
		return nil, err
	}

	event, err := s.outbox.Append(ctx, input.Event)
	if err != nil {
		return nil, err
	}

	if input.Action == store.VersionActionDelete {
		if err := s.search.RemoveIndex(ctx, input.Resource.ResourceType, input.Resource.ID); err != nil {
			return nil, err
		}
	} else {
		for _, entry := range input.SearchEntries {
			if err := s.search.RemoveIndex(ctx, entry.ResourceType, entry.ID); err != nil {
				return nil, err
			}
			if err := s.search.Index(ctx, entry); err != nil {
				return nil, err
			}
		}
	}

	return &LocalWriteResult{
		Version: input.Version,
		Event:   event,
	}, nil
}
