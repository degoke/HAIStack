package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/degoke/haistack/pkg/oauth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SQLiteRateLimitStore persists OAuth endpoint counters in SQLite.
type SQLiteRateLimitStore struct {
	DB  *sql.DB
	Now func() time.Time
}

func (s *SQLiteRateLimitStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *SQLiteRateLimitStore) Allow(ctx context.Context, endpoint, bucketKey string, limit int, window time.Duration, now time.Time) (bool, error) {
	if s == nil || s.DB == nil {
		return true, nil
	}
	if limit <= 0 {
		return true, nil
	}
	if window <= 0 {
		window = time.Minute
	}
	if now.IsZero() {
		now = s.now()
	}
	windowStart := formatOAuthTime(now.Truncate(window))

	for attempt := 0; attempt < 2; attempt++ {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return false, err
		}
		allowed, done, err := sqliteAllowInTx(ctx, tx, endpoint, bucketKey, limit, windowStart)
		if err != nil {
			_ = tx.Rollback()
			return false, err
		}
		if !done {
			_ = tx.Rollback()
			continue
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return allowed, nil
	}
	return false, errors.New("oauth rate limit: concurrent update retry exhausted")
}

func sqliteAllowInTx(ctx context.Context, tx *sql.Tx, endpoint, bucketKey string, limit int, windowStart string) (allowed, done bool, err error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE hai_oauth_rate_limit
		SET request_count = request_count + 1
		WHERE bucket_key = ? AND endpoint = ?
		  AND window_start = ?
		  AND request_count < ?`,
		bucketKey, endpoint, windowStart, limit)
	if err != nil {
		return false, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, false, err
	}
	if n == 1 {
		return true, true, nil
	}

	var storedWindow string
	var count int
	err = tx.QueryRowContext(ctx, `
		SELECT window_start, request_count
		FROM hai_oauth_rate_limit
		WHERE bucket_key = ? AND endpoint = ?`, bucketKey, endpoint).
		Scan(&storedWindow, &count)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO hai_oauth_rate_limit (bucket_key, endpoint, window_start, request_count)
			VALUES (?, ?, ?, 1)`, bucketKey, endpoint, windowStart)
		if err != nil {
			if isUniqueConstraintError(err) {
				return false, false, nil
			}
			return false, false, err
		}
		return true, true, nil
	}
	if err != nil {
		return false, false, err
	}
	if storedWindow == windowStart && count >= limit {
		return false, true, nil
	}
	if storedWindow != windowStart {
		res, err = tx.ExecContext(ctx, `
			UPDATE hai_oauth_rate_limit
			SET window_start = ?, request_count = 1
			WHERE bucket_key = ? AND endpoint = ?`,
			windowStart, bucketKey, endpoint)
		if err != nil {
			return false, false, err
		}
		n, err = res.RowsAffected()
		if err != nil {
			return false, false, err
		}
		if n == 1 {
			return true, true, nil
		}
	}
	return false, false, nil
}

// PostgresRateLimitStore persists OAuth endpoint counters in Postgres.
type PostgresRateLimitStore struct {
	Pool *pgxpool.Pool
	Now  func() time.Time
}

func (s *PostgresRateLimitStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *PostgresRateLimitStore) Allow(ctx context.Context, endpoint, bucketKey string, limit int, window time.Duration, now time.Time) (bool, error) {
	if s == nil || s.Pool == nil {
		return true, nil
	}
	if limit <= 0 {
		return true, nil
	}
	if window <= 0 {
		window = time.Minute
	}
	if now.IsZero() {
		now = s.now()
	}
	windowStart := formatOAuthTime(now.Truncate(window))

	for attempt := 0; attempt < 2; attempt++ {
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			return false, err
		}
		allowed, done, err := postgresAllowInTx(ctx, tx, endpoint, bucketKey, limit, windowStart)
		if err != nil {
			_ = tx.Rollback(ctx)
			return false, err
		}
		if !done {
			_ = tx.Rollback(ctx)
			continue
		}
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
		return allowed, nil
	}
	return false, errors.New("oauth rate limit: concurrent update retry exhausted")
}

func postgresAllowInTx(ctx context.Context, tx pgx.Tx, endpoint, bucketKey string, limit int, windowStart string) (allowed, done bool, err error) {
	tag, err := tx.Exec(ctx, `
		UPDATE hai_oauth_rate_limit
		SET request_count = request_count + 1
		WHERE bucket_key = $1 AND endpoint = $2
		  AND window_start = $3
		  AND request_count < $4`,
		bucketKey, endpoint, windowStart, limit)
	if err != nil {
		return false, false, err
	}
	if tag.RowsAffected() == 1 {
		return true, true, nil
	}

	var storedWindow string
	var count int
	err = tx.QueryRow(ctx, `
		SELECT window_start, request_count
		FROM hai_oauth_rate_limit
		WHERE bucket_key = $1 AND endpoint = $2`, bucketKey, endpoint).
		Scan(&storedWindow, &count)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `
			INSERT INTO hai_oauth_rate_limit (bucket_key, endpoint, window_start, request_count)
			VALUES ($1, $2, $3, 1)`, bucketKey, endpoint, windowStart)
		if err != nil {
			if isUniqueConstraintError(err) {
				return false, false, nil
			}
			return false, false, err
		}
		return true, true, nil
	}
	if err != nil {
		return false, false, err
	}
	if storedWindow == windowStart && count >= limit {
		return false, true, nil
	}
	if storedWindow != windowStart {
		tag, err = tx.Exec(ctx, `
			UPDATE hai_oauth_rate_limit
			SET window_start = $1, request_count = 1
			WHERE bucket_key = $2 AND endpoint = $3`,
			windowStart, bucketKey, endpoint)
		if err != nil {
			return false, false, err
		}
		if tag.RowsAffected() == 1 {
			return true, true, nil
		}
	}
	return false, false, nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "constraint failed")
}

func formatOAuthTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

var _ oauth.RateLimitStore = (*SQLiteRateLimitStore)(nil)
var _ oauth.RateLimitStore = (*PostgresRateLimitStore)(nil)
