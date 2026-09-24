package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/haistack/pkg/store"
)

// ConversationStore persists harness chat sessions for one tenant in SQLite.
type ConversationStore struct {
	exec     moduleExec
	tenantID string
}

func newConversationStore(db *sql.DB, tenantID string) *ConversationStore {
	return &ConversationStore{exec: db, tenantID: tenantID}
}

var _ store.ConversationStore = (*ConversationStore)(nil)

// Get implements store.ConversationStore.
func (s *ConversationStore) Get(ctx context.Context, id string) (*store.ConversationRecord, error) {
	row := s.exec.QueryRowContext(ctx, `
		SELECT id, tenant_id, actor, subject, messages_json, created_at, updated_at
		FROM hai_ai_conversation
		WHERE tenant_id = ? AND id = ?`, s.tenantID, id)
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
	_, err = s.exec.ExecContext(ctx, `
		INSERT INTO hai_ai_conversation (
			id, tenant_id, actor, subject, messages_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, id) DO UPDATE SET
			actor = excluded.actor,
			subject = excluded.subject,
			messages_json = excluded.messages_json,
			updated_at = excluded.updated_at`,
		record.ID,
		tenant,
		nullString(record.Actor),
		nullString(record.Subject),
		string(messages),
		formatTime(created),
		formatTime(updated),
	)
	if err != nil {
		return fmt.Errorf("put conversation: %w", err)
	}
	return nil
}

// Delete implements store.ConversationStore.
func (s *ConversationStore) Delete(ctx context.Context, id string) error {
	res, err := s.exec.ExecContext(ctx, `
		DELETE FROM hai_ai_conversation WHERE tenant_id = ? AND id = ?`, s.tenantID, id)
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	if rowsAffected(res) == 0 {
		return store.ErrConversationNotFound
	}
	return nil
}

// List implements store.ConversationStore.
func (s *ConversationStore) List(ctx context.Context, query store.ConversationQuery) ([]store.ConversationRecord, error) {
	var (
		clauses = []string{"tenant_id = ?"}
		args    = []any{s.tenantID}
	)
	if query.Actor != "" {
		clauses = append(clauses, "actor = ?")
		args = append(args, query.Actor)
	}
	if query.Subject != "" {
		clauses = append(clauses, "subject = ?")
		args = append(args, query.Subject)
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
	rows, err := s.exec.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.ConversationRecord
	for rows.Next() {
		rec, err := scanConversationRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversations: %w", err)
	}
	return out, nil
}

type conversationScanner interface {
	Scan(dest ...any) error
}

func scanConversationRecord(row conversationScanner) (*store.ConversationRecord, error) {
	var (
		id, tenant, messagesJSON, created, updated string
		actor, subject                             sql.NullString
	)
	if err := row.Scan(&id, &tenant, &actor, &subject, &messagesJSON, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrConversationNotFound
		}
		return nil, fmt.Errorf("scan conversation: %w", err)
	}
	var messages []store.ConversationMessage
	if err := json.Unmarshal([]byte(messagesJSON), &messages); err != nil {
		return nil, fmt.Errorf("unmarshal conversation messages: %w", err)
	}
	createdAt, err := parseTime(created)
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseTime(updated)
	if err != nil {
		return nil, err
	}
	rec := &store.ConversationRecord{
		ID:        id,
		TenantID:  tenant,
		Messages:  messages,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	if actor.Valid {
		rec.Actor = actor.String
	}
	if subject.Valid {
		rec.Subject = subject.String
	}
	return rec, nil
}

func rowsAffected(res sql.Result) int64 {
	if res == nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}
