package postgres

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BlobStore persists blob payloads or external location references in Postgres.
type BlobStore struct {
	exec     execer
	tenantID string
}

func newBlobStore(pool *pgxpool.Pool, tenantID string) *BlobStore {
	return &BlobStore{exec: pool, tenantID: tenantID}
}

func (s *BlobStore) Put(ctx context.Context, obj store.BlobObject) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO hai_binary_object (tenant_id, key, content_type, size, hash, data, location, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (tenant_id, key) DO UPDATE SET
			content_type = EXCLUDED.content_type,
			size = EXCLUDED.size,
			hash = EXCLUDED.hash,
			data = EXCLUDED.data,
			location = EXCLUDED.location,
			created_at = EXCLUDED.created_at`,
		s.tenantID, obj.Key, nullString(obj.ContentType), obj.Size, nullString(obj.Hash),
		obj.Data, nullString(obj.Location), obj.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("put blob: %w", err)
	}
	return nil
}

// PutStream uploads from r. The hai_binary_object.data column is BYTEA, so this
// implementation materializes the reader before INSERT. Use an object-store
// adapter (S3) or filesystem lakehouse partitions for multi-GB parquet objects.
func (s *BlobStore) PutStream(ctx context.Context, key, contentType string, size int64, r io.Reader) error {
	if r == nil {
		return fmt.Errorf("put blob stream: reader is required")
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("put blob stream: %w", err)
	}
	if size <= 0 {
		size = int64(len(data))
	}
	return s.Put(ctx, store.BlobObject{
		Key:         key,
		ContentType: contentType,
		Size:        size,
		Data:        data,
	})
}

func (s *BlobStore) Get(ctx context.Context, key string) (*store.BlobObject, error) {
	var (
		obj         store.BlobObject
		contentType *string
		hash        *string
		location    *string
		createdAt   time.Time
	)
	err := s.exec.QueryRow(ctx, `
		SELECT key, content_type, size, hash, data, location, created_at
		FROM hai_binary_object
		WHERE tenant_id = $1 AND key = $2`, s.tenantID, key,
	).Scan(&obj.Key, &contentType, &obj.Size, &hash, &obj.Data, &location, &createdAt)
	if isNoRows(err) {
		return nil, fmt.Errorf("blob not found: %s", key)
	}
	if err != nil {
		return nil, fmt.Errorf("get blob: %w", err)
	}
	if contentType != nil {
		obj.ContentType = *contentType
	}
	if hash != nil {
		obj.Hash = *hash
	}
	if location != nil {
		obj.Location = *location
	}
	obj.CreatedAt = createdAt
	return &obj, nil
}

// Open streams a blob. The BYTEA column is loaded in full, then wrapped in a
// reader; use an object-store adapter for multi-GB objects.
func (s *BlobStore) Open(ctx context.Context, key string) (io.ReadCloser, *store.BlobObject, error) {
	obj, err := s.Get(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	head := *obj
	data := head.Data
	head.Data = nil
	return io.NopCloser(bytes.NewReader(data)), &head, nil
}

func (s *BlobStore) Head(ctx context.Context, key string) (*store.BlobObject, error) {
	var (
		obj         store.BlobObject
		contentType *string
		hash        *string
		location    *string
		createdAt   time.Time
	)
	err := s.exec.QueryRow(ctx, `
		SELECT key, content_type, size, hash, location, created_at
		FROM hai_binary_object
		WHERE tenant_id = $1 AND key = $2`, s.tenantID, key,
	).Scan(&obj.Key, &contentType, &obj.Size, &hash, &location, &createdAt)
	if isNoRows(err) {
		return nil, fmt.Errorf("blob not found: %s", key)
	}
	if err != nil {
		return nil, fmt.Errorf("head blob: %w", err)
	}
	if contentType != nil {
		obj.ContentType = *contentType
	}
	if hash != nil {
		obj.Hash = *hash
	}
	if location != nil {
		obj.Location = *location
	}
	obj.CreatedAt = createdAt
	return &obj, nil
}

func (s *BlobStore) Delete(ctx context.Context, key string) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM hai_binary_object WHERE tenant_id = $1 AND key = $2`, s.tenantID, key)
	if err != nil {
		return fmt.Errorf("delete blob: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("blob not found: %s", key)
	}
	return nil
}
