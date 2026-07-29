package postgres

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/binary"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresBlobStore persists full blob bytes in Postgres via chunk tables.
// This is distinct from the legacy BlobStore backed by binary_object.
type PostgresBlobStore struct {
	exec     querier
	tenantID string
	metadata *BlobMetadataStore
}

func newPostgresBlobStore(pool *pgxpool.Pool, tenantID string) *PostgresBlobStore {
	return &PostgresBlobStore{
		exec:     pool,
		tenantID: tenantID,
		metadata: newBlobMetadataStore(pool, tenantID),
	}
}

// BlobChunkStore is kept as a compatibility alias for earlier naming.
type BlobChunkStore = PostgresBlobStore

// Put stores a finalized blob as chunked bytes in Postgres.
func (s *PostgresBlobStore) Put(ctx context.Context, blobID string, data []byte, contentType string) (*binary.BlobDescriptor, error) {
	if blobID == "" {
		return nil, fmt.Errorf("%w: blobID is required", binary.ErrInvalidArgument)
	}
	hash := binary.HashSHA256(data)
	now := time.Now().UTC()

	if err := s.putChunk(ctx, blobID, 0, data, now); err != nil {
		return nil, err
	}

	desc := &binary.BlobDescriptor{
		BlobID:      blobID,
		SHA256:      hash,
		Size:        int64(len(data)),
		ContentType: contentType,
		Backend:     binary.BackendPostgres,
		Pointer: binary.StoragePointer{
			Backend: binary.BackendPostgres,
			Ref:     blobID,
		},
	}
	manifest := binary.BlobManifest{
		Descriptor:  *desc,
		ChunkCount:  1,
		CreatedAt:   now,
		FinalizedAt: &now,
	}
	if err := s.metadata.PutManifest(ctx, manifest); err != nil {
		return nil, err
	}
	return desc, nil
}

// Get reads a finalized blob by assembling stored chunks.
func (s *PostgresBlobStore) Get(ctx context.Context, blobID string) ([]byte, *binary.BlobDescriptor, error) {
	manifest, err := s.metadata.GetManifest(ctx, blobID)
	if err != nil {
		return nil, nil, err
	}
	data, err := s.assembleChunks(ctx, blobID, manifest.ChunkCount)
	if err != nil {
		return nil, nil, err
	}
	return data, &manifest.Descriptor, nil
}

// Head returns blob metadata without reading payload bytes.
func (s *PostgresBlobStore) Head(ctx context.Context, blobID string) (*binary.BlobDescriptor, error) {
	manifest, err := s.metadata.GetManifest(ctx, blobID)
	if err != nil {
		return nil, err
	}
	return &manifest.Descriptor, nil
}

// Delete removes blob chunks and manifest.
func (s *PostgresBlobStore) Delete(ctx context.Context, blobID string) error {
	if _, err := s.metadata.GetManifest(ctx, blobID); err != nil {
		return err
	}
	if err := s.deleteChunks(ctx, blobID); err != nil {
		return err
	}
	return s.metadata.DeleteManifest(ctx, blobID)
}

// AppendChunk stores one chunk for a blob or staging key.
func (s *PostgresBlobStore) AppendChunk(ctx context.Context, key string, index int, data []byte) error {
	return s.putChunk(ctx, key, index, data, time.Now().UTC())
}

// ReadChunk reads one stored chunk.
func (s *PostgresBlobStore) ReadChunk(ctx context.Context, key string, index int) ([]byte, error) {
	var data []byte
	err := s.exec.QueryRow(ctx, `
		SELECT data FROM hai_blob_chunk
		WHERE tenant_id = $1 AND blob_id = $2 AND chunk_index = $3`,
		s.tenantID, key, index,
	).Scan(&data)
	if isNoRows(err) {
		return nil, binary.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read chunk: %w", err)
	}
	return data, nil
}

// ListChunkCount returns contiguous chunk count starting at index 0.
func (s *PostgresBlobStore) ListChunkCount(ctx context.Context, key string) (int, error) {
	rows, err := s.exec.Query(ctx, `
		SELECT chunk_index FROM hai_blob_chunk
		WHERE tenant_id = $1 AND blob_id = $2
		ORDER BY chunk_index`, s.tenantID, key,
	)
	if err != nil {
		return 0, fmt.Errorf("list chunks: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var idx int
		if err := rows.Scan(&idx); err != nil {
			return 0, fmt.Errorf("scan chunk index: %w", err)
		}
		if idx != count {
			break
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate chunks: %w", err)
	}
	return count, nil
}

// DeleteChunks removes all chunks for a key.
func (s *PostgresBlobStore) DeleteChunks(ctx context.Context, key string) error {
	return s.deleteChunks(ctx, key)
}

// Metadata returns the metadata store sharing this connection.
func (s *PostgresBlobStore) Metadata() *BlobMetadataStore {
	return s.metadata
}

func (s *PostgresBlobStore) putChunk(ctx context.Context, key string, index int, data []byte, now time.Time) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO hai_blob_chunk (tenant_id, blob_id, chunk_index, data, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, blob_id, chunk_index) DO UPDATE SET
			data = EXCLUDED.data,
			created_at = EXCLUDED.created_at`,
		s.tenantID, key, index, data, now,
	)
	if err != nil {
		return fmt.Errorf("put chunk: %w", err)
	}
	return nil
}

func (s *PostgresBlobStore) deleteChunks(ctx context.Context, key string) error {
	_, err := s.exec.Exec(ctx, `
		DELETE FROM hai_blob_chunk WHERE tenant_id = $1 AND blob_id = $2`,
		s.tenantID, key,
	)
	if err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}
	return nil
}

func (s *PostgresBlobStore) assembleChunks(ctx context.Context, key string, count int) ([]byte, error) {
	if count <= 0 {
		count, _ = s.ListChunkCount(ctx, key)
	}
	if count == 0 {
		return nil, binary.ErrNotFound
	}
	var buf bytes.Buffer
	for i := 0; i < count; i++ {
		chunk, err := s.ReadChunk(ctx, key, i)
		if err != nil {
			return nil, err
		}
		if _, err := buf.Write(chunk); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}
