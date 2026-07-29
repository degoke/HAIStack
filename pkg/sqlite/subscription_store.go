package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// SubscriptionStore persists subscription definitions in SQLite.
type SubscriptionStore struct {
	db *sql.DB
}

func newSubscriptionStore(db *sql.DB) *SubscriptionStore {
	return &SubscriptionStore{db: db}
}

var _ store.SubscriptionStore = (*SubscriptionStore)(nil)

// Create implements store.SubscriptionStore.
func (s *SubscriptionStore) Create(ctx context.Context, record store.SubscriptionRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO hai_subscription_registry (
			id, name, status, resource_type, event_kind,
			trigger_json, channel_json, retry_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.Name, string(record.Status), record.ResourceType, record.EventKind,
		string(record.TriggerJSON), string(record.ChannelJSON), nullBytes(record.RetryJSON),
		formatTime(record.CreatedAt), formatTime(record.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}
	return nil
}

// Update implements store.SubscriptionStore.
func (s *SubscriptionStore) Update(ctx context.Context, record store.SubscriptionRecord) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE hai_subscription_registry
		SET name = ?, status = ?, resource_type = ?, event_kind = ?,
			trigger_json = ?, channel_json = ?, retry_json = ?, updated_at = ?
		WHERE id = ?`,
		record.Name, string(record.Status), record.ResourceType, record.EventKind,
		string(record.TriggerJSON), string(record.ChannelJSON), nullBytes(record.RetryJSON),
		formatTime(record.UpdatedAt), record.ID,
	)
	if err != nil {
		return fmt.Errorf("update subscription: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update subscription rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("subscription not found: %s", record.ID)
	}
	return nil
}

// Get implements store.SubscriptionStore.
func (s *SubscriptionStore) Get(ctx context.Context, id string) (*store.SubscriptionRecord, error) {
	rec, err := scanSubscription(s.db.QueryRowContext(ctx, `
		SELECT id, name, status, resource_type, event_kind,
			trigger_json, channel_json, retry_json, created_at, updated_at
		FROM hai_subscription_registry WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("subscription not found: %s", id)
	}
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// List implements store.SubscriptionStore.
func (s *SubscriptionStore) List(ctx context.Context, query store.SubscriptionListQuery) ([]store.SubscriptionRecord, error) {
	var clauses []string
	var args []any
	if query.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, string(query.Status))
	}
	if query.ResourceType != "" {
		clauses = append(clauses, "resource_type = ?")
		args = append(args, query.ResourceType)
	}
	if query.EventKind != "" {
		clauses = append(clauses, "event_kind = ?")
		args = append(args, query.EventKind)
	}
	where := "1=1"
	if len(clauses) > 0 {
		where = strings.Join(clauses, " AND ")
	}
	sqlQuery := fmt.Sprintf(`
		SELECT id, name, status, resource_type, event_kind,
			trigger_json, channel_json, retry_json, created_at, updated_at
		FROM hai_subscription_registry
		WHERE %s
		ORDER BY created_at ASC`, where)
	if query.Limit > 0 {
		sqlQuery += fmt.Sprintf(" LIMIT %d", query.Limit)
	}
	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.SubscriptionRecord
	for rows.Next() {
		rec, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriptions: %w", err)
	}
	return out, nil
}

// Delete implements store.SubscriptionStore.
func (s *SubscriptionStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM hai_subscription_registry WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete subscription rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("subscription not found: %s", id)
	}
	return nil
}

type subscriptionScanner interface {
	Scan(dest ...any) error
}

func scanSubscription(row subscriptionScanner) (*store.SubscriptionRecord, error) {
	var (
		rec         store.SubscriptionRecord
		status      string
		triggerJSON string
		channelJSON string
		retryJSON   sql.NullString
		createdAt   string
		updatedAt   string
	)
	if err := row.Scan(
		&rec.ID, &rec.Name, &status, &rec.ResourceType, &rec.EventKind,
		&triggerJSON, &channelJSON, &retryJSON, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	rec.Status = store.SubscriptionStatus(status)
	rec.TriggerJSON = []byte(triggerJSON)
	rec.ChannelJSON = []byte(channelJSON)
	if retryJSON.Valid {
		rec.RetryJSON = []byte(retryJSON.String)
	}
	var err error
	if rec.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, fmt.Errorf("parse subscription created_at: %w", err)
	}
	if rec.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, fmt.Errorf("parse subscription updated_at: %w", err)
	}
	return &rec, nil
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}
