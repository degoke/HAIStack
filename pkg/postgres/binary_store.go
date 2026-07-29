package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BinaryStore persists inline binary payloads in Postgres.
type BinaryStore struct {
	exec     execer
	tenantID string
}

func newBinaryStore(pool *pgxpool.Pool, tenantID string) *BinaryStore {
	return &BinaryStore{exec: pool, tenantID: tenantID}
}

func (s *BinaryStore) Put(ctx context.Context, obj store.BinaryObject) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO hai_binary_object (tenant_id, key, content_type, size, hash, data, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (tenant_id, key) DO UPDATE SET
			content_type = EXCLUDED.content_type,
			size = EXCLUDED.size,
			hash = EXCLUDED.hash,
			data = EXCLUDED.data,
			created_at = EXCLUDED.created_at`,
		s.tenantID, obj.Key, nullString(obj.ContentType), obj.Size, nullString(obj.Hash), obj.Data, obj.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("put binary: %w", err)
	}
	return nil
}

func (s *BinaryStore) Get(ctx context.Context, key string) (*store.BinaryObject, error) {
	var (
		obj         store.BinaryObject
		contentType *string
		hash        *string
		createdAt   time.Time
	)
	err := s.exec.QueryRow(ctx, `
		SELECT key, content_type, size, hash, data, created_at
		FROM hai_binary_object
		WHERE tenant_id = $1 AND key = $2`, s.tenantID, key,
	).Scan(&obj.Key, &contentType, &obj.Size, &hash, &obj.Data, &createdAt)
	if isNoRows(err) {
		return nil, fmt.Errorf("binary not found: %s", key)
	}
	if err != nil {
		return nil, fmt.Errorf("get binary: %w", err)
	}
	if contentType != nil {
		obj.ContentType = *contentType
	}
	if hash != nil {
		obj.Hash = *hash
	}
	obj.CreatedAt = createdAt
	return &obj, nil
}

func (s *BinaryStore) Delete(ctx context.Context, key string) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM hai_binary_object WHERE tenant_id = $1 AND key = $2`, s.tenantID, key)
	if err != nil {
		return fmt.Errorf("delete binary: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("binary not found: %s", key)
	}
	return nil
}
