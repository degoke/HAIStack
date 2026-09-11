package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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
	windowStart := formatTime(now.Truncate(window))

	for attempt := 0; attempt < 2; attempt++ {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return false, err
		}
		allowed, done, err := s.allowInTx(ctx, tx, endpoint, bucketKey, limit, windowStart)
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

func (s *SQLRateLimitStore) allowInTx(ctx context.Context, tx sqlTx, endpoint, bucketKey string, limit int, windowStart string) (allowed, done bool, err error) {
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
	if err == sql.ErrNoRows {
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

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "constraint failed")
}

var _ oauth.RateLimitStore = (*SQLRateLimitStore)(nil)
