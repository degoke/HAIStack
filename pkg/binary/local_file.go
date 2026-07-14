package binary

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// LocalFileBlobStore stores finalized blobs and upload chunks on the local filesystem
// under hash-addressed paths.
type LocalFileBlobStore struct {
	root string
}

// NewLocalFileBlobStore creates a filesystem blob store rooted at root.
func NewLocalFileBlobStore(root string) (*LocalFileBlobStore, error) {
	if root == "" {
		return nil, fmt.Errorf("%w: root path is required", ErrInvalidArgument)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create blob root: %w", err)
	}
	return &LocalFileBlobStore{root: root}, nil
}

func (s *LocalFileBlobStore) blobPath(sha256 string) string {
	if len(sha256) < 4 {
		return filepath.Join(s.root, "blobs", sha256)
	}
	return filepath.Join(s.root, "blobs", sha256[:2], sha256[2:4], sha256)
}

func (s *LocalFileBlobStore) chunkDir(key string) string {
	return filepath.Join(s.root, "chunks", key)
}

func (s *LocalFileBlobStore) chunkPath(key string, index int) string {
	return filepath.Join(s.chunkDir(key), fmt.Sprintf("%06d", index))
}

// Put stores data as a finalized blob keyed by content hash.
func (s *LocalFileBlobStore) Put(ctx context.Context, blobID string, data []byte, contentType string) (*BlobDescriptor, error) {
	_ = ctx
	if blobID == "" {
		return nil, fmt.Errorf("%w: blobID is required", ErrInvalidArgument)
	}
	hash := HashSHA256(data)
	path := s.blobPath(hash)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create blob dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, fmt.Errorf("write blob: %w", err)
	}
	return &BlobDescriptor{
		BlobID:      blobID,
		SHA256:      hash,
		Size:        int64(len(data)),
		ContentType: contentType,
		Backend:     BackendLocalFile,
		Pointer: StoragePointer{
			Backend: BackendLocalFile,
			Ref:     path,
		},
	}, nil
}

// Get reads a finalized blob by blobID using the stored hash path.
func (s *LocalFileBlobStore) Get(ctx context.Context, blobID string, sha256 string) ([]byte, error) {
	_ = ctx
	_ = blobID
	if sha256 == "" {
		return nil, fmt.Errorf("%w: sha256 is required", ErrInvalidArgument)
	}
	data, err := os.ReadFile(s.blobPath(sha256))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read blob: %w", err)
	}
	return data, nil
}

// GetByHash reads finalized blob bytes by content hash.
func (s *LocalFileBlobStore) GetByHash(ctx context.Context, sha256 string) ([]byte, error) {
	return s.Get(ctx, "", sha256)
}

// Head returns metadata for a finalized blob without reading payload bytes.
func (s *LocalFileBlobStore) Head(ctx context.Context, blobID string, sha256 string, contentType string) (*BlobDescriptor, error) {
	_ = ctx
	if sha256 == "" {
		return nil, fmt.Errorf("%w: sha256 is required", ErrInvalidArgument)
	}
	path := s.blobPath(sha256)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("stat blob: %w", err)
	}
	return &BlobDescriptor{
		BlobID:      blobID,
		SHA256:      sha256,
		Size:        info.Size(),
		ContentType: contentType,
		Backend:     BackendLocalFile,
		Pointer: StoragePointer{
			Backend: BackendLocalFile,
			Ref:     path,
		},
	}, nil
}

// Delete removes a finalized blob by content hash.
func (s *LocalFileBlobStore) Delete(ctx context.Context, sha256 string) error {
	_ = ctx
	if sha256 == "" {
		return fmt.Errorf("%w: sha256 is required", ErrInvalidArgument)
	}
	path := s.blobPath(sha256)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("delete blob: %w", err)
	}
	return nil
}

// AppendChunk stores one upload chunk for a session or staging key.
func (s *LocalFileBlobStore) AppendChunk(ctx context.Context, key string, index int, data []byte) error {
	_ = ctx
	if key == "" {
		return fmt.Errorf("%w: chunk key is required", ErrInvalidArgument)
	}
	if index < 0 {
		return fmt.Errorf("%w: chunk index must be non-negative", ErrInvalidArgument)
	}
	dir := s.chunkDir(key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create chunk dir: %w", err)
	}
	path := s.chunkPath(key, index)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write chunk: %w", err)
	}
	return nil
}

// ReadChunk reads one stored chunk.
func (s *LocalFileBlobStore) ReadChunk(ctx context.Context, key string, index int) ([]byte, error) {
	_ = ctx
	data, err := os.ReadFile(s.chunkPath(key, index))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read chunk: %w", err)
	}
	return data, nil
}

// ListChunkCount returns the number of contiguous chunks starting at index 0.
func (s *LocalFileBlobStore) ListChunkCount(ctx context.Context, key string) (int, error) {
	_ = ctx
	dir := s.chunkDir(key)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("list chunks: %w", err)
	}
	if len(entries) == 0 {
		return 0, nil
	}
	var indices []int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".tmp") {
			idx, err := strconv.Atoi(name)
			if err != nil {
				continue
			}
			indices = append(indices, idx)
		}
	}
	sort.Ints(indices)
	count := 0
	for i, idx := range indices {
		if idx != i {
			break
		}
		count++
	}
	return count, nil
}

// DeleteChunks removes all chunks for a staging key.
func (s *LocalFileBlobStore) DeleteChunks(ctx context.Context, key string) error {
	_ = ctx
	dir := s.chunkDir(key)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}
	return nil
}

// AssembleChunks concatenates staged chunks into a single byte slice.
func (s *LocalFileBlobStore) AssembleChunks(ctx context.Context, key string) ([]byte, error) {
	count, err := s.ListChunkCount(ctx, key)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, ErrUploadIncomplete
	}
	var buf []byte
	for i := 0; i < count; i++ {
		chunk, err := s.ReadChunk(ctx, key, i)
		if err != nil {
			return nil, err
		}
		buf = append(buf, chunk...)
	}
	return buf, nil
}

// FinalizeChunks assembles staged chunks, stores the finalized blob, and removes staging chunks.
func (s *LocalFileBlobStore) FinalizeChunks(ctx context.Context, key string, blobID string, contentType string) (*BlobManifest, error) {
	data, err := s.AssembleChunks(ctx, key)
	if err != nil {
		return nil, err
	}
	desc, err := s.Put(ctx, blobID, data, contentType)
	if err != nil {
		return nil, err
	}
	if err := s.DeleteChunks(ctx, key); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &BlobManifest{
		Descriptor:  *desc,
		ChunkCount:  1,
		CreatedAt:   now,
		FinalizedAt: &now,
	}, nil
}

// OpenBlob opens a finalized blob for streaming read.
func (s *LocalFileBlobStore) OpenBlob(sha256 string) (io.ReadCloser, error) {
	f, err := os.Open(s.blobPath(sha256))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("open blob: %w", err)
	}
	return f, nil
}

// Compile-time interface checks.
var (
	_ BlobStore  = (*localFileBlobStoreAdapter)(nil)
	_ ChunkStore = (*LocalFileBlobStore)(nil)
)

// localFileBlobStoreAdapter adapts LocalFileBlobStore to BlobStore using manifest metadata.
type localFileBlobStoreAdapter struct {
	files    *LocalFileBlobStore
	manifest MetadataStore
}

// NewLocalFileBlobStoreAdapter returns a BlobStore backed by LocalFileBlobStore and MetadataStore.
func NewLocalFileBlobStoreAdapter(files *LocalFileBlobStore, manifest MetadataStore) BlobStore {
	return &localFileBlobStoreAdapter{files: files, manifest: manifest}
}

func (a *localFileBlobStoreAdapter) Put(ctx context.Context, blobID string, data []byte, contentType string) (*BlobDescriptor, error) {
	desc, err := a.files.Put(ctx, blobID, data, contentType)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	manifest := BlobManifest{
		Descriptor:  *desc,
		ChunkCount:  1,
		CreatedAt:   now,
		FinalizedAt: &now,
	}
	if err := a.manifest.PutManifest(ctx, manifest); err != nil {
		return nil, err
	}
	return desc, nil
}

func (a *localFileBlobStoreAdapter) Get(ctx context.Context, blobID string) ([]byte, *BlobDescriptor, error) {
	m, err := a.manifest.GetManifest(ctx, blobID)
	if err != nil {
		return nil, nil, err
	}
	data, err := a.files.GetByHash(ctx, m.Descriptor.SHA256)
	if err != nil {
		return nil, nil, err
	}
	return data, &m.Descriptor, nil
}

func (a *localFileBlobStoreAdapter) Head(ctx context.Context, blobID string) (*BlobDescriptor, error) {
	m, err := a.manifest.GetManifest(ctx, blobID)
	if err != nil {
		return nil, err
	}
	desc, err := a.files.Head(ctx, blobID, m.Descriptor.SHA256, m.Descriptor.ContentType)
	if err != nil {
		return nil, err
	}
	return desc, nil
}

func (a *localFileBlobStoreAdapter) Delete(ctx context.Context, blobID string) error {
	m, err := a.manifest.GetManifest(ctx, blobID)
	if err != nil {
		return err
	}
	if err := a.files.Delete(ctx, m.Descriptor.SHA256); err != nil && err != ErrNotFound {
		return err
	}
	return a.manifest.DeleteManifest(ctx, blobID)
}
