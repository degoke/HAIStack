package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// SubscriptionDeliveryStore persists delivery logs in SQLite.
type SubscriptionDeliveryStore struct {
	db *sql.DB
}

func newSubscriptionDeliveryStore(db *sql.DB) *SubscriptionDeliveryStore {
	return &SubscriptionDeliveryStore{db: db}
}

var _ store.SubscriptionDeliveryStore = (*SubscriptionDeliveryStore)(nil)

// Append implements store.SubscriptionDeliveryStore.
func (s *SubscriptionDeliveryStore) Append(ctx context.Context, record store.DeliveryRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO subscription_delivery_log (
			id, subscription_id, event_sequence, attempt, status,
			response_status, response_body, error_message, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.SubscriptionID, record.EventSequence, record.Attempt, string(record.Status),
		nullIntArg(record.ResponseStatus), nullString(record.ResponseBody), nullString(record.ErrorMessage),
		formatTime(record.CreatedAt), formatTime(record.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("append delivery: %w", err)
	}
	return nil
}

// Update implements store.SubscriptionDeliveryStore.
func (s *SubscriptionDeliveryStore) Update(ctx context.Context, record store.DeliveryRecord) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE subscription_delivery_log
		SET status = ?, response_status = ?, response_body = ?, error_message = ?, updated_at = ?
		WHERE id = ?`,
		string(record.Status), nullIntArg(record.ResponseStatus), nullString(record.ResponseBody),
		nullString(record.ErrorMessage), formatTime(record.UpdatedAt), record.ID,
	)
	if err != nil {
		return fmt.Errorf("update delivery: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update delivery rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("delivery not found: %s", record.ID)
	}
	return nil
}

// Get implements store.SubscriptionDeliveryStore.
func (s *SubscriptionDeliveryStore) Get(ctx context.Context, id string) (*store.DeliveryRecord, error) {
	rec, err := scanDelivery(s.db.QueryRowContext(ctx, `
		SELECT id, subscription_id, event_sequence, attempt, status,
			response_status, response_body, error_message, created_at, updated_at
		FROM subscription_delivery_log WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("delivery not found: %s", id)
	}
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// List implements store.SubscriptionDeliveryStore.
func (s *SubscriptionDeliveryStore) List(ctx context.Context, query store.DeliveryListQuery) ([]store.DeliveryRecord, error) {
	var clauses []string
	var args []any
	if query.SubscriptionID != "" {
		clauses = append(clauses, "subscription_id = ?")
		args = append(args, query.SubscriptionID)
	}
	if query.EventSequence > 0 {
		clauses = append(clauses, "event_sequence = ?")
		args = append(args, query.EventSequence)
	}
	where := "1=1"
	if len(clauses) > 0 {
		where = ""
		for i, c := range clauses {
			if i > 0 {
				where += " AND "
			}
			where += c
		}
	}
	sqlQuery := fmt.Sprintf(`
		SELECT id, subscription_id, event_sequence, attempt, status,
			response_status, response_body, error_message, created_at, updated_at
		FROM subscription_delivery_log
		WHERE %s
		ORDER BY created_at ASC`, where)
	if query.Limit > 0 {
		sqlQuery += fmt.Sprintf(" LIMIT %d", query.Limit)
	}
	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	defer func() { _ = rows.Close() }()
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

type deliveryScanner interface {
	Scan(dest ...any) error
}

func scanDelivery(row deliveryScanner) (*store.DeliveryRecord, error) {
	var (
		rec            store.DeliveryRecord
		status         string
		responseStatus sql.NullInt64
		responseBody   sql.NullString
		errorMessage   sql.NullString
		createdAt      string
		updatedAt      string
	)
	if err := row.Scan(
		&rec.ID, &rec.SubscriptionID, &rec.EventSequence, &rec.Attempt, &status,
		&responseStatus, &responseBody, &errorMessage, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	rec.Status = store.DeliveryStatus(status)
	if responseStatus.Valid {
		rec.ResponseStatus = int(responseStatus.Int64)
	}
	if responseBody.Valid {
		rec.ResponseBody = responseBody.String
	}
	if errorMessage.Valid {
		rec.ErrorMessage = errorMessage.String
	}
	var err error
	if rec.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, fmt.Errorf("parse delivery created_at: %w", err)
	}
	if rec.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, fmt.Errorf("parse delivery updated_at: %w", err)
	}
	return &rec, nil
}

func nullIntArg(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
