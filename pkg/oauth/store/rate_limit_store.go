package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

// SQLRateLimitStore persists OAuth endpoint counters in SQL.
type SQLRateLimitStore struct {
	DB      SQLDB
	Dialect Dialect
	Now     func() time.Time
}

func (s *SQLRateLimitStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *SQLRateLimitStore) Allow(ctx context.Context, endpoint, bucketKey string, limit int, window time.Duration, now time.Time) (bool, error) {
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
	windowStart := now.Truncate(window)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var storedWindow string
	var count int
	err = tx.QueryRowContext(ctx, `
		SELECT window_start, request_count
		FROM hai_oauth_rate_limit
		WHERE bucket_key = ? AND endpoint = ?`, bucketKey, endpoint).
		Scan(&storedWindow, &count)
	if err == sql.ErrNoRows {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO hai_oauth_rate_limit (bucket_key, endpoint, window_start, request_count)
			VALUES (?, ?, ?, 1)`, bucketKey, endpoint, formatTime(windowStart))
		if err != nil {
			return false, err
		}
		return tx.Commit() == nil, nil
	}
	if err != nil {
		return false, err
	}
	currentWindow, _ := parseTime(storedWindow)
	if !currentWindow.Equal(windowStart) {
		_, err = tx.ExecContext(ctx, `
			UPDATE hai_oauth_rate_limit
			SET window_start = ?, request_count = 1
			WHERE bucket_key = ? AND endpoint = ?`,
			formatTime(windowStart), bucketKey, endpoint)
		if err != nil {
			return false, err
		}
		return tx.Commit() == nil, nil
	}
	if count >= limit {
		return false, nil
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE hai_oauth_rate_limit
		SET request_count = request_count + 1
		WHERE bucket_key = ? AND endpoint = ?`, bucketKey, endpoint)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

var _ oauth.RateLimitStore = (*SQLRateLimitStore)(nil)
