package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/binary"
)

// SQLiteBlobStore persists full blob bytes in SQLite via chunk and manifest tables.
type SQLiteBlobStore struct {
	exec     queryExec
	metadata *BlobMetadataStore
}

func newSQLiteBlobStore(db *sql.DB) *SQLiteBlobStore {
	return &SQLiteBlobStore{
		exec:     db,
		metadata: newBlobMetadataStore(db),
	}
}

func newSQLiteBlobStoreTx(tx *sql.Tx) *SQLiteBlobStore {
	return &SQLiteBlobStore{
		exec:     tx,
		metadata: newBlobMetadataStoreTx(tx),
	}
}

// ChunkBlobStore is kept as a compatibility alias for earlier naming.
type ChunkBlobStore = SQLiteBlobStore

// Put stores a finalized blob as chunked bytes in SQLite.
func (s *SQLiteBlobStore) Put(ctx context.Context, blobID string, data []byte, contentType string) (*binary.BlobDescriptor, error) {
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
		Backend:     binary.BackendSQLite,
		Pointer: binary.StoragePointer{
			Backend: binary.BackendSQLite,
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
func (s *SQLiteBlobStore) Get(ctx context.Context, blobID string) ([]byte, *binary.BlobDescriptor, error) {
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
func (s *SQLiteBlobStore) Head(ctx context.Context, blobID string) (*binary.BlobDescriptor, error) {
	manifest, err := s.metadata.GetManifest(ctx, blobID)
	if err != nil {
		return nil, err
	}
	return &manifest.Descriptor, nil
}

// Delete removes blob chunks and manifest.
func (s *SQLiteBlobStore) Delete(ctx context.Context, blobID string) error {
	if _, err := s.metadata.GetManifest(ctx, blobID); err != nil {
		return err
	}
	if err := s.deleteChunks(ctx, blobID); err != nil {
		return err
	}
	return s.metadata.DeleteManifest(ctx, blobID)
}

// AppendChunk stores one chunk for a blob or staging key.
func (s *SQLiteBlobStore) AppendChunk(ctx context.Context, key string, index int, data []byte) error {
	return s.putChunk(ctx, key, index, data, time.Now().UTC())
}

// ReadChunk reads one stored chunk.
func (s *SQLiteBlobStore) ReadChunk(ctx context.Context, key string, index int) ([]byte, error) {
	var data []byte
	err := s.exec.QueryRowContext(ctx, `
		SELECT data FROM blob_chunk WHERE blob_id = ? AND chunk_index = ?`,
		key, index,
	).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, binary.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read chunk: %w", err)
	}
	return data, nil
}

// ListChunkCount returns contiguous chunk count starting at index 0.
func (s *SQLiteBlobStore) ListChunkCount(ctx context.Context, key string) (int, error) {
	rows, err := s.exec.QueryContext(ctx, `
		SELECT chunk_index FROM blob_chunk WHERE blob_id = ? ORDER BY chunk_index`,
		key,
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
func (s *SQLiteBlobStore) DeleteChunks(ctx context.Context, key string) error {
	return s.deleteChunks(ctx, key)
}

func (s *SQLiteBlobStore) putChunk(ctx context.Context, key string, index int, data []byte, now time.Time) error {
	_, err := s.exec.ExecContext(ctx, `
		INSERT INTO blob_chunk (blob_id, chunk_index, data, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(blob_id, chunk_index) DO UPDATE SET
			data = excluded.data,
			created_at = excluded.created_at`,
		key, index, data, formatTime(now),
	)
	if err != nil {
		return fmt.Errorf("put chunk: %w", err)
	}
	return nil
}

func (s *SQLiteBlobStore) deleteChunks(ctx context.Context, key string) error {
	_, err := s.exec.ExecContext(ctx, `DELETE FROM blob_chunk WHERE blob_id = ?`, key)
	if err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}
	return nil
}

func (s *SQLiteBlobStore) assembleChunks(ctx context.Context, key string, count int) ([]byte, error) {
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

// Metadata returns the metadata store sharing this connection.
func (s *SQLiteBlobStore) Metadata() *BlobMetadataStore {
	return s.metadata
}
