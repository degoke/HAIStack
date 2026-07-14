package binary_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/degoke/health-ai-stack/pkg/binary"
)

// Compile-time interface checks for memory test doubles.
var (
	_ binary.BlobStore      = (*memBlobStore)(nil)
	_ binary.ChunkStore     = (*memChunkStore)(nil)
	_ binary.MetadataStore  = (*memMetadataStore)(nil)
	_ binary.TransferStore  = (*memTransferStore)(nil)
)

type memBlobStore struct {
	mu    sync.Mutex
	blobs map[string]struct {
		data        []byte
		contentType string
	}
}

func newMemBlobStore() *memBlobStore {
	return &memBlobStore{blobs: make(map[string]struct {
		data        []byte
		contentType string
	})}
}

func (m *memBlobStore) Put(ctx context.Context, blobID string, data []byte, contentType string) (*binary.BlobDescriptor, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobs[blobID] = struct {
		data        []byte
		contentType string
	}{append([]byte(nil), data...), contentType}
	hash := binary.HashSHA256(data)
	return &binary.BlobDescriptor{
		BlobID: blobID, SHA256: hash, Size: int64(len(data)),
		ContentType: contentType, Backend: binary.BackendLocalFile,
		Pointer: binary.StoragePointer{Backend: binary.BackendLocalFile, Ref: blobID},
	}, nil
}

func (m *memBlobStore) Get(ctx context.Context, blobID string) ([]byte, *binary.BlobDescriptor, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.blobs[blobID]
	if !ok {
		return nil, nil, binary.ErrNotFound
	}
	desc := &binary.BlobDescriptor{
		BlobID: blobID, SHA256: binary.HashSHA256(entry.data), Size: int64(len(entry.data)),
		ContentType: entry.contentType, Backend: binary.BackendLocalFile,
		Pointer: binary.StoragePointer{Backend: binary.BackendLocalFile, Ref: blobID},
	}
	return append([]byte(nil), entry.data...), desc, nil
}

func (m *memBlobStore) Head(ctx context.Context, blobID string) (*binary.BlobDescriptor, error) {
	_, desc, err := m.Get(ctx, blobID)
	return desc, err
}

func (m *memBlobStore) Delete(ctx context.Context, blobID string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.blobs[blobID]; !ok {
		return binary.ErrNotFound
	}
	delete(m.blobs, blobID)
	return nil
}

type memChunkStore struct {
	mu     sync.Mutex
	chunks map[string]map[int][]byte
}

func newMemChunkStore() *memChunkStore {
	return &memChunkStore{chunks: make(map[string]map[int][]byte)}
}

func (m *memChunkStore) AppendChunk(ctx context.Context, key string, index int, data []byte) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.chunks[key] == nil {
		m.chunks[key] = make(map[int][]byte)
	}
	m.chunks[key][index] = append([]byte(nil), data...)
	return nil
}

func (m *memChunkStore) ReadChunk(ctx context.Context, key string, index int) ([]byte, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	chunks, ok := m.chunks[key]
	if !ok {
		return nil, binary.ErrNotFound
	}
	data, ok := chunks[index]
	if !ok {
		return nil, binary.ErrNotFound
	}
	return append([]byte(nil), data...), nil
}

func (m *memChunkStore) ListChunkCount(ctx context.Context, key string) (int, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	chunks := m.chunks[key]
	count := 0
	for {
		if _, ok := chunks[count]; !ok {
			break
		}
		count++
	}
	return count, nil
}

func (m *memChunkStore) DeleteChunks(ctx context.Context, key string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.chunks, key)
	return nil
}

type memMetadataStore struct {
	mu        sync.Mutex
	manifests map[string]binary.BlobManifest
	byHash    map[string]string
	binLinks  map[string]binary.BinaryLink
	docLinks  map[string]binary.DocumentAttachmentLink
	sync      map[string]binary.BlobSyncStatus
}

func newMemMetadataStore() *memMetadataStore {
	return &memMetadataStore{
		manifests: make(map[string]binary.BlobManifest),
		byHash:    make(map[string]string),
		binLinks:  make(map[string]binary.BinaryLink),
		docLinks:  make(map[string]binary.DocumentAttachmentLink),
		sync:      make(map[string]binary.BlobSyncStatus),
	}
}

func (m *memMetadataStore) PutManifest(ctx context.Context, manifest binary.BlobManifest) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.manifests[manifest.Descriptor.BlobID] = manifest
	m.byHash[manifest.Descriptor.SHA256] = manifest.Descriptor.BlobID
	return nil
}

func (m *memMetadataStore) GetManifest(ctx context.Context, blobID string) (*binary.BlobManifest, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	manifest, ok := m.manifests[blobID]
	if !ok {
		return nil, binary.ErrNotFound
	}
	return &manifest, nil
}

func (m *memMetadataStore) GetManifestByHash(ctx context.Context, sha256 string) (*binary.BlobManifest, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	blobID, ok := m.byHash[sha256]
	if !ok {
		return nil, binary.ErrNotFound
	}
	manifest := m.manifests[blobID]
	return &manifest, nil
}

func (m *memMetadataStore) DeleteManifest(ctx context.Context, blobID string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	manifest, ok := m.manifests[blobID]
	if !ok {
		return binary.ErrNotFound
	}
	delete(m.byHash, manifest.Descriptor.SHA256)
	delete(m.manifests, blobID)
	return nil
}

func (m *memMetadataStore) PutBinaryLink(ctx context.Context, link binary.BinaryLink) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.binLinks[link.ResourceID] = link
	return nil
}

func (m *memMetadataStore) GetBinaryLink(ctx context.Context, resourceID string) (*binary.BinaryLink, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	link, ok := m.binLinks[resourceID]
	if !ok {
		return nil, binary.ErrNotFound
	}
	return &link, nil
}

func (m *memMetadataStore) DeleteBinaryLink(ctx context.Context, resourceID string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.binLinks[resourceID]; !ok {
		return binary.ErrNotFound
	}
	delete(m.binLinks, resourceID)
	return nil
}

func (m *memMetadataStore) PutDocumentLink(ctx context.Context, link binary.DocumentAttachmentLink) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	key := docLinkKey(link.DocumentID, link.ContentIndex)
	m.docLinks[key] = link
	return nil
}

func (m *memMetadataStore) GetDocumentLink(ctx context.Context, documentID string, contentIndex int) (*binary.DocumentAttachmentLink, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	link, ok := m.docLinks[docLinkKey(documentID, contentIndex)]
	if !ok {
		return nil, binary.ErrNotFound
	}
	return &link, nil
}

func (m *memMetadataStore) DeleteDocumentLink(ctx context.Context, documentID string, contentIndex int) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	key := docLinkKey(documentID, contentIndex)
	if _, ok := m.docLinks[key]; !ok {
		return binary.ErrNotFound
	}
	delete(m.docLinks, key)
	return nil
}

func (m *memMetadataStore) PutSyncStatus(ctx context.Context, blobID string, status binary.BlobSyncStatus) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sync[blobID] = status
	return nil
}

func (m *memMetadataStore) GetSyncStatus(ctx context.Context, blobID string) (binary.BlobSyncStatus, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	status, ok := m.sync[blobID]
	if !ok {
		return "", binary.ErrNotFound
	}
	return status, nil
}

func (m *memMetadataStore) DeleteSyncStatus(ctx context.Context, blobID string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sync[blobID]; !ok {
		return binary.ErrNotFound
	}
	delete(m.sync, blobID)
	return nil
}

func docLinkKey(documentID string, contentIndex int) string {
	return fmt.Sprintf("%s:%d", documentID, contentIndex)
}

type memTransferStore struct {
	mu        sync.Mutex
	uploads   map[string]binary.UploadSession
	downloads map[string]binary.DownloadSession
}

func newMemTransferStore() *memTransferStore {
	return &memTransferStore{
		uploads:   make(map[string]binary.UploadSession),
		downloads: make(map[string]binary.DownloadSession),
	}
}

func (m *memTransferStore) CreateUploadSession(ctx context.Context, session binary.UploadSession) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploads[session.ID] = session
	return nil
}

func (m *memTransferStore) GetUploadSession(ctx context.Context, id string) (*binary.UploadSession, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.uploads[id]
	if !ok {
		return nil, binary.ErrNotFound
	}
	return &session, nil
}

func (m *memTransferStore) UpdateUploadSession(ctx context.Context, session binary.UploadSession) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.uploads[session.ID]; !ok {
		return binary.ErrNotFound
	}
	m.uploads[session.ID] = session
	return nil
}

func (m *memTransferStore) DeleteUploadSession(ctx context.Context, id string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.uploads[id]; !ok {
		return binary.ErrNotFound
	}
	delete(m.uploads, id)
	return nil
}

func (m *memTransferStore) CreateDownloadSession(ctx context.Context, session binary.DownloadSession) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.downloads[session.ID] = session
	return nil
}

func (m *memTransferStore) GetDownloadSession(ctx context.Context, id string) (*binary.DownloadSession, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.downloads[id]
	if !ok {
		return nil, binary.ErrNotFound
	}
	return &session, nil
}

func (m *memTransferStore) UpdateDownloadSession(ctx context.Context, session binary.DownloadSession) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.downloads[session.ID]; !ok {
		return binary.ErrNotFound
	}
	m.downloads[session.ID] = session
	return nil
}

func (m *memTransferStore) DeleteDownloadSession(ctx context.Context, id string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.downloads[id]; !ok {
		return binary.ErrNotFound
	}
	delete(m.downloads, id)
	return nil
}

func fixedTime() time.Time {
	return time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
}
