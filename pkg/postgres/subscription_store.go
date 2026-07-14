package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SubscriptionStore persists subscription definitions in Postgres.
type SubscriptionStore struct {
	exec     querier
	tenantID string
}

func newSubscriptionStore(pool *pgxpool.Pool, tenantID string) *SubscriptionStore {
	return &SubscriptionStore{exec: pool, tenantID: tenantID}
}

func newSubscriptionStoreTx(tx querier, tenantID string) *SubscriptionStore {
	return &SubscriptionStore{exec: tx, tenantID: tenantID}
}

var _ store.SubscriptionStore = (*SubscriptionStore)(nil)

// Create implements store.SubscriptionStore.
func (s *SubscriptionStore) Create(ctx context.Context, record store.SubscriptionRecord) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO subscription_registry (
			id, tenant_id, name, status, resource_type, event_kind,
			trigger_json, channel_json, retry_json, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		record.ID, s.tenantID, record.Name, string(record.Status), record.ResourceType, record.EventKind,
		record.TriggerJSON, record.ChannelJSON, nullableJSON(record.RetryJSON),
		record.CreatedAt, record.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}
	return nil
}

// Update implements store.SubscriptionStore.
func (s *SubscriptionStore) Update(ctx context.Context, record store.SubscriptionRecord) error {
	tag, err := s.exec.Exec(ctx, `
		UPDATE subscription_registry
		SET name = $1, status = $2, resource_type = $3, event_kind = $4,
			trigger_json = $5, channel_json = $6, retry_json = $7, updated_at = $8
		WHERE tenant_id = $9 AND id = $10`,
		record.Name, string(record.Status), record.ResourceType, record.EventKind,
		record.TriggerJSON, record.ChannelJSON, nullableJSON(record.RetryJSON), record.UpdatedAt,
		s.tenantID, record.ID,
	)
	if err != nil {
		return fmt.Errorf("update subscription: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("subscription not found: %s", record.ID)
	}
	return nil
}

// Get implements store.SubscriptionStore.
func (s *SubscriptionStore) Get(ctx context.Context, id string) (*store.SubscriptionRecord, error) {
	row := s.exec.QueryRow(ctx, `
		SELECT id, name, status, resource_type, event_kind,
			trigger_json, channel_json, retry_json, created_at, updated_at
		FROM subscription_registry
		WHERE tenant_id = $1 AND id = $2`, s.tenantID, id)
	rec, err := scanSubscription(row)
	if isNoRows(err) {
		return nil, fmt.Errorf("subscription not found: %s", id)
	}
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// List implements store.SubscriptionStore.
func (s *SubscriptionStore) List(ctx context.Context, query store.SubscriptionListQuery) ([]store.SubscriptionRecord, error) {
	var (
		clauses []string
		args    []any
		argN    = 1
	)
	clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", argN))
	args = append(args, s.tenantID)
	argN++
	if query.Status != "" {
		clauses = append(clauses, fmt.Sprintf("status = $%d", argN))
		args = append(args, string(query.Status))
		argN++
	}
	if query.ResourceType != "" {
		clauses = append(clauses, fmt.Sprintf("resource_type = $%d", argN))
		args = append(args, query.ResourceType)
		argN++
	}
	if query.EventKind != "" {
		clauses = append(clauses, fmt.Sprintf("event_kind = $%d", argN))
		args = append(args, query.EventKind)
		argN++
	}
	where := strings.Join(clauses, " AND ")
	sqlQuery := fmt.Sprintf(`
		SELECT id, name, status, resource_type, event_kind,
			trigger_json, channel_json, retry_json, created_at, updated_at
		FROM subscription_registry
		WHERE %s
		ORDER BY created_at ASC`, where)
	if query.Limit > 0 {
		sqlQuery += fmt.Sprintf(" LIMIT %d", query.Limit)
	}
	rows, err := s.exec.Query(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	defer rows.Close()
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
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM subscription_registry WHERE tenant_id = $1 AND id = $2`, s.tenantID, id)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("subscription not found: %s", id)
	}
	return nil
}

type subscriptionRowScanner interface {
	Scan(dest ...any) error
}

func scanSubscription(row subscriptionRowScanner) (*store.SubscriptionRecord, error) {
	var (
		rec        store.SubscriptionRecord
		status     string
		retryJSON  []byte
	)
	if err := row.Scan(
		&rec.ID, &rec.Name, &status, &rec.ResourceType, &rec.EventKind,
		&rec.TriggerJSON, &rec.ChannelJSON, &retryJSON, &rec.CreatedAt, &rec.UpdatedAt,
	); err != nil {
		return nil, err
	}
	rec.Status = store.SubscriptionStatus(status)
	rec.RetryJSON = retryJSON
	return &rec, nil
}

func nullableJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
