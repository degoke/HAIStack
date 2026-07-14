package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SubscriptionDeliveryStore persists delivery logs in Postgres.
type SubscriptionDeliveryStore struct {
	exec     querier
	tenantID string
}

func newSubscriptionDeliveryStore(pool *pgxpool.Pool, tenantID string) *SubscriptionDeliveryStore {
	return &SubscriptionDeliveryStore{exec: pool, tenantID: tenantID}
}

func newSubscriptionDeliveryStoreTx(tx querier, tenantID string) *SubscriptionDeliveryStore {
	return &SubscriptionDeliveryStore{exec: tx, tenantID: tenantID}
}

var _ store.SubscriptionDeliveryStore = (*SubscriptionDeliveryStore)(nil)

// Append implements store.SubscriptionDeliveryStore.
func (s *SubscriptionDeliveryStore) Append(ctx context.Context, record store.DeliveryRecord) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO subscription_delivery_log (
			id, tenant_id, subscription_id, event_sequence, attempt, status,
			response_status, response_body, error_message, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		record.ID, s.tenantID, record.SubscriptionID, record.EventSequence, record.Attempt, string(record.Status),
		nullDeliveryInt32(record.ResponseStatus), nullString(record.ResponseBody), nullString(record.ErrorMessage),
		record.CreatedAt, record.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("append delivery: %w", err)
	}
	return nil
}

// Update implements store.SubscriptionDeliveryStore.
func (s *SubscriptionDeliveryStore) Update(ctx context.Context, record store.DeliveryRecord) error {
	tag, err := s.exec.Exec(ctx, `
		UPDATE subscription_delivery_log
		SET status = $1, response_status = $2, response_body = $3, error_message = $4, updated_at = $5
		WHERE tenant_id = $6 AND id = $7`,
		string(record.Status), nullDeliveryInt32(record.ResponseStatus), nullString(record.ResponseBody),
		nullString(record.ErrorMessage), record.UpdatedAt, s.tenantID, record.ID,
	)
	if err != nil {
		return fmt.Errorf("update delivery: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delivery not found: %s", record.ID)
	}
	return nil
}

// Get implements store.SubscriptionDeliveryStore.
func (s *SubscriptionDeliveryStore) Get(ctx context.Context, id string) (*store.DeliveryRecord, error) {
	row := s.exec.QueryRow(ctx, `
		SELECT id, subscription_id, event_sequence, attempt, status,
			response_status, response_body, error_message, created_at, updated_at
		FROM subscription_delivery_log
		WHERE tenant_id = $1 AND id = $2`, s.tenantID, id)
	rec, err := scanDelivery(row)
	if isNoRows(err) {
		return nil, fmt.Errorf("delivery not found: %s", id)
	}
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// List implements store.SubscriptionDeliveryStore.
func (s *SubscriptionDeliveryStore) List(ctx context.Context, query store.DeliveryListQuery) ([]store.DeliveryRecord, error) {
	var (
		clauses []string
		args    []any
		argN    = 1
	)
	clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", argN))
	args = append(args, s.tenantID)
	argN++
	if query.SubscriptionID != "" {
		clauses = append(clauses, fmt.Sprintf("subscription_id = $%d", argN))
		args = append(args, query.SubscriptionID)
		argN++
	}
	if query.EventSequence > 0 {
		clauses = append(clauses, fmt.Sprintf("event_sequence = $%d", argN))
		args = append(args, query.EventSequence)
		argN++
	}
	where := strings.Join(clauses, " AND ")
	sqlQuery := fmt.Sprintf(`
		SELECT id, subscription_id, event_sequence, attempt, status,
			response_status, response_body, error_message, created_at, updated_at
		FROM subscription_delivery_log
		WHERE %s
		ORDER BY created_at ASC`, where)
	if query.Limit > 0 {
		sqlQuery += fmt.Sprintf(" LIMIT %d", query.Limit)
	}
	rows, err := s.exec.Query(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	defer rows.Close()
	var out []store.DeliveryRecord
	for rows.Next() {
		rec, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deliveries: %w", err)
	}
	return out, nil
}

type deliveryRowScanner interface {
	Scan(dest ...any) error
}

func scanDelivery(row deliveryRowScanner) (*store.DeliveryRecord, error) {
	var (
		rec            store.DeliveryRecord
		status         string
		responseStatus *int32
		responseBody   *string
		errorMessage   *string
	)
	if err := row.Scan(
		&rec.ID, &rec.SubscriptionID, &rec.EventSequence, &rec.Attempt, &status,
		&responseStatus, &responseBody, &errorMessage, &rec.CreatedAt, &rec.UpdatedAt,
	); err != nil {
		return nil, err
	}
	rec.Status = store.DeliveryStatus(status)
	if responseStatus != nil {
		rec.ResponseStatus = int(*responseStatus)
	}
	if responseBody != nil {
		rec.ResponseBody = *responseBody
	}
	if errorMessage != nil {
		rec.ErrorMessage = *errorMessage
	}
	return &rec, nil
}

func nullDeliveryInt32(v int) *int32 {
	if v == 0 {
		return nil
	}
	n := int32(v)
	return &n
}
