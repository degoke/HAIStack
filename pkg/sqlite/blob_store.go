package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"time"

	"github.com/degoke/haistack/pkg/binary"
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

// ChunkBlobStore is kept as a compatibility alias for earlier naming.
type ChunkBlobStore = SQLiteBlobStore

// Put stores a finalized blob as chunked bytes in SQLite.
func (s *SQLiteBlobStore) Put(ctx context.Context, blobID string, data []byte, contentType string) (*binary.BlobDescriptor, error) {
	return s.PutStream(ctx, blobID, bytes.NewReader(data), int64(len(data)), contentType)
}

// PutStream stores a finalized blob by reading r in DefaultChunkSize slices.
func (s *SQLiteBlobStore) PutStream(ctx context.Context, blobID string, r io.Reader, size int64, contentType string) (*binary.BlobDescriptor, error) {
	if blobID == "" {
		return nil, fmt.Errorf("%w: blobID is required", binary.ErrInvalidArgument)
	}
	_ = size
	now := time.Now().UTC()
	if err := s.deleteChunks(ctx, blobID); err != nil {
		return nil, err
	}
	hash, total, count, err := binary.CopyChunks(r, binary.DefaultChunkSize, func(index int, chunk []byte) error {
		return s.putChunk(ctx, blobID, index, chunk, now)
	})
	if err != nil {
		_ = s.deleteChunks(ctx, blobID)
		return nil, err
	}

	desc := &binary.BlobDescriptor{
		BlobID:      blobID,
		SHA256:      hash,
		Size:        total,
		ContentType: contentType,
		Backend:     binary.BackendSQLite,
		Pointer: binary.StoragePointer{
			Backend: binary.BackendSQLite,
			Ref:     blobID,
		},
	}
	manifest := binary.BlobManifest{
		Descriptor:  *desc,
		ChunkSize:   int64(binary.DefaultChunkSize),
		ChunkCount:  count,
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
	rc, desc, err := s.Open(ctx, blobID)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, nil, err
	}
	return data, desc, nil
}

// Open streams a finalized blob one chunk at a time.
func (s *SQLiteBlobStore) Open(ctx context.Context, blobID string) (io.ReadCloser, *binary.BlobDescriptor, error) {
	manifest, err := s.metadata.GetManifest(ctx, blobID)
	if err != nil {
		return nil, nil, err
	}
	desc := manifest.Descriptor
	r := binary.NewChunkReader(ctx, manifest.ChunkCount, func(ctx context.Context, index int) ([]byte, error) {
		return s.ReadChunk(ctx, blobID, index)
	})
	return r, &desc, nil
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
		SELECT data FROM hai_blob_chunk WHERE blob_id = ? AND chunk_index = ?`,
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
		SELECT chunk_index FROM hai_blob_chunk WHERE blob_id = ? ORDER BY chunk_index`,
		key,
	)
	if err != nil {
		return 0, fmt.Errorf("list chunks: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
		INSERT INTO hai_blob_chunk (blob_id, chunk_index, data, created_at)
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
	_, err := s.exec.ExecContext(ctx, `DELETE FROM hai_blob_chunk WHERE blob_id = ?`, key)
	if err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}
	return nil
}

// Metadata returns the metadata store sharing this connection.
func (s *SQLiteBlobStore) Metadata() *BlobMetadataStore {
	return s.metadata
}
