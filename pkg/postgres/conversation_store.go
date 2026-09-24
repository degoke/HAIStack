package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/haistack/pkg/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ConversationStore persists harness chat sessions for one tenant in Postgres.
type ConversationStore struct {
	exec     querier
	tenantID string
}

func newConversationStore(pool *pgxpool.Pool, tenantID string) *ConversationStore {
	return &ConversationStore{exec: pool, tenantID: tenantID}
}

// Get implements store.ConversationStore.
func (s *ConversationStore) Get(ctx context.Context, id string) (*store.ConversationRecord, error) {
	row := s.exec.QueryRow(ctx, `
		SELECT id, tenant_id, actor, subject, messages_json, created_at, updated_at
		FROM hai_ai_conversation
		WHERE tenant_id = $1 AND id = $2`, s.tenantID, id)
	return scanConversationRecord(row)
}

// Put implements store.ConversationStore.
func (s *ConversationStore) Put(ctx context.Context, record store.ConversationRecord) error {
	if strings.TrimSpace(record.ID) == "" {
		return fmt.Errorf("conversation id is required")
	}
	tenant := s.tenantID
	if record.TenantID != "" {
		tenant = record.TenantID
	}
	messages, err := json.Marshal(record.Messages)
	if err != nil {
		return fmt.Errorf("marshal conversation messages: %w", err)
	}
	created := record.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	updated := record.UpdatedAt
	if updated.IsZero() {
		updated = created
	}
	_, err = s.exec.Exec(ctx, `
		INSERT INTO hai_ai_conversation (
			id, tenant_id, actor, subject, messages_json, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (tenant_id, id) DO UPDATE SET
			actor = EXCLUDED.actor,
			subject = EXCLUDED.subject,
			messages_json = EXCLUDED.messages_json,
			updated_at = EXCLUDED.updated_at`,
		record.ID,
		tenant,
		nullString(record.Actor),
		nullString(record.Subject),
		messages,
		created,
		updated,
	)
	if err != nil {
		return fmt.Errorf("put conversation: %w", err)
	}
	return nil
}

// Delete implements store.ConversationStore.
func (s *ConversationStore) Delete(ctx context.Context, id string) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM hai_ai_conversation WHERE tenant_id = $1 AND id = $2`, s.tenantID, id)
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrConversationNotFound
	}
	return nil
}

// List implements store.ConversationStore.
func (s *ConversationStore) List(ctx context.Context, query store.ConversationQuery) ([]store.ConversationRecord, error) {
	var (
		clauses = []string{"tenant_id = $1"}
		args    = []any{s.tenantID}
		argN    = 2
	)
	if query.Actor != "" {
		clauses = append(clauses, fmt.Sprintf("actor = $%d", argN))
		args = append(args, query.Actor)
		argN++
	}
	if query.Subject != "" {
		clauses = append(clauses, fmt.Sprintf("subject = $%d", argN))
		args = append(args, query.Subject)
		argN++
	}
	where := strings.Join(clauses, " AND ")
	sqlQuery := fmt.Sprintf(`
		SELECT id, tenant_id, actor, subject, messages_json, created_at, updated_at
		FROM hai_ai_conversation
		WHERE %s
		ORDER BY updated_at DESC`, where)
	if query.Limit > 0 {
		sqlQuery += fmt.Sprintf(" LIMIT %d", query.Limit)
	}
	rows, err := s.exec.Query(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()
	var out []store.ConversationRecord
	for rows.Next() {
		rec, err := scanConversationRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, rows.Err()
}

func scanConversationRecord(row pgx.Row) (*store.ConversationRecord, error) {
	var (
		id, tenantID string
		actor        *string
		subject      *string
		messagesJSON []byte
		created      time.Time
		updated      time.Time
	)
	if err := row.Scan(&id, &tenantID, &actor, &subject, &messagesJSON, &created, &updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrConversationNotFound
		}
		return nil, fmt.Errorf("scan conversation: %w", err)
	}
	var messages []store.ConversationMessage
	if err := json.Unmarshal(messagesJSON, &messages); err != nil {
		return nil, fmt.Errorf("unmarshal conversation messages: %w", err)
	}
	rec := &store.ConversationRecord{
		ID:        id,
		TenantID:  tenantID,
		Messages:  messages,
		CreatedAt: created,
		UpdatedAt: updated,
	}
	if actor != nil {
		rec.Actor = *actor
	}
	if subject != nil {
		rec.Subject = *subject
	}
	return rec, nil
}
