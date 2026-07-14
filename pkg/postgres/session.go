package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/binary"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// WriteOutcome describes whether a write was accepted, rejected, or conflicted.
type WriteOutcome string

const (
	WriteOutcomeAccepted   WriteOutcome = "accepted"
	WriteOutcomeRejected   WriteOutcome = "rejected"
	WriteOutcomeConflicted WriteOutcome = "conflicted"
)

// Write bundles inputs for one atomic write pipeline.
type Write struct {
	Resource      *types.ResourceEnvelope
	Action        store.VersionAction
	SearchEntries []store.SearchIndexEntry
	RequestedID   string
	Audit         store.AuditRecord

	Outcome                 WriteOutcome
	RejectionReason         string
	ConflictLocalVersionID  string
	ConflictRemoteVersionID string
}

// WriteResult returns persisted write outputs.
type WriteResult struct {
	Resource   *types.ResourceEnvelope
	Version    store.ResourceVersion
	Event      store.ResourceEvent
	IDRegistry store.IDRegistryResult
	Outcome    WriteOutcome
}

// Session coordinates transaction-scoped store access for atomic writes.
type Session struct {
	tx       pgx.Tx
	tenantID string

	resource  *ResourceStore
	history   *HistoryStore
	search    *SearchStore
	events    *EventStore
	cursor    *CursorStore
	idReg     *IDRegistry
	conflicts *ConflictStore
	audit     *AuditStore

	committed  bool
	rolledBack bool
}

func newSession(tx pgx.Tx, tenantID string) *Session {
	return &Session{
		tx:        tx,
		tenantID:  tenantID,
		resource:  newResourceStoreTx(tx, tenantID),
		history:   newHistoryStoreTx(tx, tenantID),
		search:    newSearchStoreTx(tx, tenantID),
		events:    newEventStoreTx(tx, tenantID),
		cursor:    newCursorStoreTx(tx, tenantID),
		idReg:     newIDRegistryTx(tx, tenantID),
		conflicts: newConflictStoreTx(tx, tenantID),
		audit:     newAuditStoreTx(tx, tenantID),
	}
}

// Commit commits the session transaction.
func (s *Session) Commit(ctx context.Context) error {
	if s.committed {
		return fmt.Errorf("session already committed")
	}
	if s.rolledBack {
		return fmt.Errorf("session already rolled back")
	}
	if err := s.tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit session: %w", err)
	}
	s.committed = true
	return nil
}

// Rollback rolls back the session transaction.
func (s *Session) Rollback(ctx context.Context) error {
	if s.committed {
		return fmt.Errorf("session already committed")
	}
	if s.rolledBack {
		return nil
	}
	if err := s.tx.Rollback(ctx); err != nil {
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

// EventStore returns the transaction-scoped event store.
func (s *Session) EventStore() store.EventStore {
	return s.events
}

// CursorStore returns the transaction-scoped cursor store.
func (s *Session) CursorStore() store.CursorStore {
	return s.cursor
}

// IDRegistry returns the transaction-scoped ID registry store.
func (s *Session) IDRegistry() store.IDRegistryStore {
	return s.idReg
}

// ConflictStore returns the transaction-scoped conflict store.
func (s *Session) ConflictStore() store.ConflictStore {
	return s.conflicts
}

// AuditStore returns the transaction-scoped audit store.
func (s *Session) AuditStore() store.AuditStore {
	return s.audit
}

// BinaryMetadataStore returns the transaction-scoped blob metadata store.
func (s *Session) BinaryMetadataStore() binary.MetadataStore {
	return newBlobMetadataStoreTx(s.tx, s.tenantID)
}

func (s *Session) applyWrite(ctx context.Context, input Write) (*WriteResult, error) {
	outcome := input.Outcome
	if outcome == "" {
		outcome = WriteOutcomeAccepted
	}

	if input.Audit.ID == "" {
		input.Audit.ID = uuid.NewString()
	}
	if input.Audit.Timestamp.IsZero() {
		input.Audit.Timestamp = time.Now().UTC()
	}

	switch outcome {
	case WriteOutcomeRejected, WriteOutcomeConflicted:
		return s.applyRejectedWrite(ctx, input, outcome)
	case WriteOutcomeAccepted:
		return s.applyAcceptedWrite(ctx, input)
	default:
		return nil, fmt.Errorf("unsupported write outcome %q", outcome)
	}
}

func (s *Session) applyRejectedWrite(ctx context.Context, input Write, outcome WriteOutcome) (*WriteResult, error) {
	if input.Resource == nil {
		return nil, fmt.Errorf("resource envelope is nil")
	}

	input.Audit.Outcome = string(outcome)
	input.Audit.ResourceType = input.Resource.ResourceType
	input.Audit.ResourceID = input.Resource.ID
	if err := s.audit.Append(ctx, input.Audit); err != nil {
		return nil, err
	}

	if outcome == WriteOutcomeConflicted {
		conflictID := uuid.NewString()
		if err := s.conflicts.Append(ctx, store.ConflictRecord{
			ID:              conflictID,
			ResourceType:    input.Resource.ResourceType,
			ResourceID:      input.Resource.ID,
			LocalVersionID:  input.ConflictLocalVersionID,
			RemoteVersionID: input.ConflictRemoteVersionID,
			Reason:          input.RejectionReason,
			CreatedAt:       time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
	}

	return &WriteResult{
		Resource: input.Resource,
		Outcome:  outcome,
	}, nil
}

func (s *Session) applyAcceptedWrite(ctx context.Context, input Write) (*WriteResult, error) {
	if input.Resource == nil {
		return nil, fmt.Errorf("resource envelope is nil")
	}

	resourceID := input.Resource.ID
	if input.RequestedID != "" {
		resourceID = input.RequestedID
		input.Resource.ID = resourceID
	}

	idResult := store.IDRegistryResult{
		ResourceType: input.Resource.ResourceType,
		ID:           resourceID,
	}

	switch input.Action {
	case store.VersionActionCreate:
		if err := s.idReg.Reserve(ctx, input.Resource.ResourceType, resourceID); err != nil {
			return nil, err
		}
		idResult.Registered = true
	case store.VersionActionUpdate, store.VersionActionDelete:
		registered, err := s.idReg.Check(ctx, input.Resource.ResourceType, resourceID)
		if err != nil {
			return nil, err
		}
		if !registered {
			if err := s.idReg.Register(ctx, input.Resource.ResourceType, resourceID); err != nil {
				return nil, err
			}
		}
		idResult.Registered = true
	default:
		return nil, fmt.Errorf("unsupported version action %q", input.Action)
	}

	versionID := uuid.NewString()
	now := time.Now().UTC()
	if input.Resource.LastUpdated.IsZero() {
		input.Resource.LastUpdated = now
	}
	input.Resource.VersionID = versionID

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
		if err := s.resource.Delete(ctx, input.Resource.ResourceType, resourceID); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported version action %q", input.Action)
	}

	version := store.ResourceVersion{
		ResourceType: input.Resource.ResourceType,
		ID:           resourceID,
		VersionID:    versionID,
		Action:       input.Action,
		Timestamp:    now,
		Hash:         input.Resource.Hash,
	}
	if input.Action == store.VersionActionDelete {
		version.Deleted = true
	} else {
		version.Resource = input.Resource
	}

	if err := s.history.AppendVersion(ctx, version); err != nil {
		return nil, err
	}

	eventAction := store.EventAction(input.Action)
	event, err := s.events.Append(ctx, store.ResourceEvent{
		ResourceType: input.Resource.ResourceType,
		ID:           resourceID,
		VersionID:    versionID,
		Action:       eventAction,
		Timestamp:    now,
		Hash:         input.Resource.Hash,
	})
	if err != nil {
		return nil, err
	}

	if input.Action == store.VersionActionDelete {
		if err := s.search.RemoveIndex(ctx, input.Resource.ResourceType, resourceID); err != nil {
			return nil, err
		}
	} else {
		for _, entry := range input.SearchEntries {
			entry.ID = resourceID
			if err := s.search.RemoveIndex(ctx, entry.ResourceType, entry.ID); err != nil {
				return nil, err
			}
			if err := s.search.Index(ctx, entry); err != nil {
				return nil, err
			}
		}
	}

	input.Audit.Outcome = string(WriteOutcomeAccepted)
	input.Audit.ResourceType = input.Resource.ResourceType
	input.Audit.ResourceID = resourceID
	if err := s.audit.Append(ctx, input.Audit); err != nil {
		return nil, err
	}

	written := *input.Resource
	return &WriteResult{
		Resource:   &written,
		Version:    version,
		Event:      event,
		IDRegistry: idResult,
		Outcome:    WriteOutcomeAccepted,
	}, nil
}
