package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// BinaryStore persists small inline binary payloads in SQLite.
//
// Inline keyed storage only; large blob offload and deduplication are out of scope here.
type BinaryStore struct {
	exec queryExec
}

func newBinaryStore(db *sql.DB) *BinaryStore {
	return &BinaryStore{exec: db}
}

//nolint:unused // used by transactional write sessions
func newBinaryStoreTx(tx *sql.Tx) *BinaryStore {
	return &BinaryStore{exec: tx}
}

func (s *BinaryStore) Put(ctx context.Context, obj store.BinaryObject) error {
	_, err := s.exec.ExecContext(ctx, `
		INSERT INTO hai_binary_object (key, content_type, size, hash, data, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			content_type = excluded.content_type,
			size = excluded.size,
			hash = excluded.hash,
			data = excluded.data,
			created_at = excluded.created_at`,
		obj.Key, obj.ContentType, obj.Size, obj.Hash, obj.Data, formatTime(obj.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("put binary: %w", err)
	}
	return nil
}

func (s *BinaryStore) Get(ctx context.Context, key string) (*store.BinaryObject, error) {
	var (
		obj         store.BinaryObject
		contentType sql.NullString
		hash        sql.NullString
		createdAt   string
	)
	err := s.exec.QueryRowContext(ctx, `
		SELECT key, content_type, size, hash, data, created_at
		FROM hai_binary_object WHERE key = ?`, key,
	).Scan(&obj.Key, &contentType, &obj.Size, &hash, &obj.Data, &createdAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("binary not found: %s", key)
	}
	if err != nil {
		return nil, fmt.Errorf("get binary: %w", err)
	}
	if contentType.Valid {
		obj.ContentType = contentType.String
	}
	if hash.Valid {
		obj.Hash = hash.String
	}
	ts, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	obj.CreatedAt = ts
	return &obj, nil
}

func (s *BinaryStore) Delete(ctx context.Context, key string) error {
	result, err := s.exec.ExecContext(ctx, `DELETE FROM hai_binary_object WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("delete binary: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete binary rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("binary not found: %s", key)
	}
	return nil
}
