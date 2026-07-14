package binary

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DefaultChunkSize is the default chunk size for resumable transfers (1 MiB).
const DefaultChunkSize = 1 << 20

// TransferService orchestrates resumable upload and download using chunk and transfer stores.
type TransferService struct {
	blobs      BlobStore
	chunks     ChunkStore
	meta       MetadataStore
	transfers  TransferStore
	chunkSize  int64
	resolveKey KeyResolver
}

// TransferConfig configures a TransferService.
type TransferConfig struct {
	Blobs      BlobStore
	Chunks     ChunkStore
	Metadata   MetadataStore
	Transfers  TransferStore
	ChunkSize  int64
	ResolveKey KeyResolver
}

// NewTransferService creates a transfer orchestrator.
func NewTransferService(cfg TransferConfig) (*TransferService, error) {
	if cfg.Blobs == nil || cfg.Chunks == nil || cfg.Metadata == nil || cfg.Transfers == nil {
		return nil, fmt.Errorf("%w: blobs, chunks, metadata, and transfers are required", ErrInvalidArgument)
	}
	chunkSize := cfg.ChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	return &TransferService{
		blobs:      cfg.Blobs,
		chunks:     cfg.Chunks,
		meta:       cfg.Metadata,
		transfers:  cfg.Transfers,
		chunkSize:  chunkSize,
		resolveKey: cfg.ResolveKey,
	}, nil
}

// StartUpload begins a resumable upload session.
func (s *TransferService) StartUpload(ctx context.Context, blobID string, size int64, contentType string, expectedChunks int) (*UploadSession, error) {
	return s.StartUploadWithOptions(ctx, UploadRequest{
		BlobID:         blobID,
		Size:           size,
		ContentType:    contentType,
		ExpectedChunks: expectedChunks,
	})
}

// StartUploadWithOptions begins a resumable upload session with retention and encryption metadata.
func (s *TransferService) StartUploadWithOptions(ctx context.Context, req UploadRequest) (*UploadSession, error) {
	blobID := req.BlobID
	if blobID == "" {
		blobID = uuid.NewString()
	}
	now := time.Now().UTC()
	session := UploadSession{
		ID:             uuid.NewString(),
		BlobID:         blobID,
		Size:           req.Size,
		ContentType:    req.ContentType,
		ChunkSize:      s.chunkSize,
		ExpectedChunks: req.ExpectedChunks,
		Status:         SyncUploading,
		Retention:      req.Retention,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if req.EncryptionKeyID != "" {
		session.Encryption = &BlobEncryption{
			Algorithm: EncryptionAES256GCM,
			KeyID:     req.EncryptionKeyID,
		}
	}
	if err := s.transfers.CreateUploadSession(ctx, session); err != nil {
		return nil, err
	}
	if err := s.meta.PutSyncStatus(ctx, blobID, SyncUploading); err != nil {
		return nil, err
	}
	return &session, nil
}

// UploadChunk appends one chunk to an active upload session.
func (s *TransferService) UploadChunk(ctx context.Context, sessionID string, index int, data []byte) (*UploadSession, error) {
	session, err := s.transfers.GetUploadSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session.Status != SyncUploading && session.Status != SyncPending {
		return nil, ErrSessionClosed
	}
	key := uploadChunkKey(sessionID)
	if err := s.chunks.AppendChunk(ctx, key, index, data); err != nil {
		return nil, err
	}
	session.UploadedChunks++
	session.UploadedBytes += int64(len(data))
	session.Status = SyncUploading
	session.UpdatedAt = time.Now().UTC()
	if err := s.transfers.UpdateUploadSession(ctx, *session); err != nil {
		return nil, err
	}
	return session, nil
}

// FinalizeUpload assembles chunks, deduplicates by hash, and records the manifest.
func (s *TransferService) FinalizeUpload(ctx context.Context, sessionID string) (*BlobManifest, error) {
	session, err := s.transfers.GetUploadSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	key := uploadChunkKey(sessionID)
	count, err := s.chunks.ListChunkCount(ctx, key)
	if err != nil {
		return nil, err
	}
	if session.ExpectedChunks > 0 && count < session.ExpectedChunks {
		return nil, ErrUploadIncomplete
	}
	if count == 0 {
		return nil, ErrUploadIncomplete
	}

	data, err := assembleChunks(ctx, s.chunks, key, count)
	if err != nil {
		return nil, err
	}
	hash := HashSHA256(data)

	if existing, err := s.meta.GetManifestByHash(ctx, hash); err == nil {
		_ = s.chunks.DeleteChunks(ctx, key)
		session.SHA256 = hash
		session.Status = SyncUploaded
		session.UpdatedAt = time.Now().UTC()
		_ = s.transfers.UpdateUploadSession(ctx, *session)
		_ = s.meta.PutSyncStatus(ctx, session.BlobID, SyncComplete)
		return existing, nil
	}

	storeBytes := data
	encryption := session.Encryption
	if session.Encryption != nil {
		encrypted, encMeta, err := encryptBytes(ctx, data, session.Encryption, s.resolveKey)
		if err != nil {
			session.Status = SyncFailed
			session.UpdatedAt = time.Now().UTC()
			_ = s.transfers.UpdateUploadSession(ctx, *session)
			_ = s.meta.PutSyncStatus(ctx, session.BlobID, SyncFailed)
			return nil, err
		}
		storeBytes = encrypted
		encryption = encMeta
	}

	var desc *BlobDescriptor
	if advanced, ok := s.blobs.(BlobStoreWithOptions); ok {
		desc, err = advanced.PutWithOptions(ctx, session.BlobID, storeBytes, BlobWriteOptions{
			ContentType: session.ContentType,
			Retention:   session.Retention,
		})
	} else {
		desc, err = s.blobs.Put(ctx, session.BlobID, storeBytes, session.ContentType)
	}
	if err != nil {
		session.Status = SyncFailed
		session.UpdatedAt = time.Now().UTC()
		_ = s.transfers.UpdateUploadSession(ctx, *session)
		_ = s.meta.PutSyncStatus(ctx, session.BlobID, SyncFailed)
		return nil, err
	}
	_ = s.chunks.DeleteChunks(ctx, key)

	now := time.Now().UTC()
	manifest := BlobManifest{
		Descriptor:  *desc,
		ChunkSize:   session.ChunkSize,
		ChunkCount:  count,
		CreatedAt:   session.CreatedAt,
		FinalizedAt: &now,
	}
	manifest.Descriptor.SHA256 = hash
	manifest.Descriptor.Size = session.Size
	manifest.Descriptor.ContentType = session.ContentType
	manifest.Descriptor.Encryption = encryption
	manifest.Descriptor.Retention = session.Retention
	if err := s.meta.PutManifest(ctx, manifest); err != nil {
		return nil, err
	}

	session.SHA256 = hash
	session.Encryption = encryption
	session.Status = SyncUploaded
	session.UpdatedAt = now
	if err := s.transfers.UpdateUploadSession(ctx, *session); err != nil {
		return nil, err
	}
	if err := s.meta.PutSyncStatus(ctx, session.BlobID, SyncComplete); err != nil {
		return nil, err
	}
	return &manifest, nil
}

// StartDownload begins a resumable download session for a finalized blob.
func (s *TransferService) StartDownload(ctx context.Context, blobID string) (*DownloadSession, error) {
	manifest, err := s.meta.GetManifest(ctx, blobID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	session := DownloadSession{
		ID:          uuid.NewString(),
		BlobID:      blobID,
		ChunkSize:   s.chunkSize,
		TotalChunks: manifest.ChunkCount,
		Status:      SyncDownloading,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if manifest.ChunkCount == 0 {
		session.TotalChunks = 1
	}
	if err := s.transfers.CreateDownloadSession(ctx, session); err != nil {
		return nil, err
	}
	if err := s.meta.PutSyncStatus(ctx, blobID, SyncDownloading); err != nil {
		return nil, err
	}
	return &session, nil
}

// DownloadChunk reads the next chunk for a download session.
func (s *TransferService) DownloadChunk(ctx context.Context, sessionID string, index int) ([]byte, *DownloadSession, error) {
	session, err := s.transfers.GetDownloadSession(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	if session.Status != SyncDownloading && session.Status != SyncComplete {
		return nil, nil, ErrSessionClosed
	}

	data, _, err := s.blobs.Get(ctx, session.BlobID)
	if err != nil {
		session.Status = SyncFailed
		session.UpdatedAt = time.Now().UTC()
		_ = s.transfers.UpdateDownloadSession(ctx, *session)
		_ = s.meta.PutSyncStatus(ctx, session.BlobID, SyncFailed)
		return nil, nil, err
	}
	manifest, err := s.meta.GetManifest(ctx, session.BlobID)
	if err != nil {
		return nil, nil, err
	}
	if manifest.Descriptor.Encryption != nil {
		data, err = decryptBytes(ctx, data, manifest.Descriptor.Encryption, s.resolveKey)
		if err != nil {
			session.Status = SyncFailed
			session.UpdatedAt = time.Now().UTC()
			_ = s.transfers.UpdateDownloadSession(ctx, *session)
			_ = s.meta.PutSyncStatus(ctx, session.BlobID, SyncFailed)
			return nil, nil, err
		}
	}

	start := int64(index) * session.ChunkSize
	if start >= int64(len(data)) {
		return nil, session, ErrNotFound
	}
	end := start + session.ChunkSize
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	chunk := data[start:end]

	session.DownloadedChunks = index + 1
	session.DownloadedBytes += int64(len(chunk))
	session.UpdatedAt = time.Now().UTC()
	if session.DownloadedBytes >= int64(len(data)) {
		session.Status = SyncComplete
		_ = s.meta.PutSyncStatus(ctx, session.BlobID, SyncComplete)
	}
	if err := s.transfers.UpdateDownloadSession(ctx, *session); err != nil {
		return nil, nil, err
	}
	return chunk, session, nil
}

// MarkSyncPending sets a blob to pending sync status.
func (s *TransferService) MarkSyncPending(ctx context.Context, blobID string) error {
	return s.meta.PutSyncStatus(ctx, blobID, SyncPending)
}

func uploadChunkKey(sessionID string) string {
	return "upload:" + sessionID
}

func assembleChunks(ctx context.Context, chunks ChunkStore, key string, count int) ([]byte, error) {
	var buf bytes.Buffer
	for i := 0; i < count; i++ {
		chunk, err := chunks.ReadChunk(ctx, key, i)
		if err != nil {
			return nil, err
		}
		if _, err := buf.Write(chunk); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// ReadBlob returns the fully assembled plaintext blob bytes for managed access.
func (s *TransferService) ReadBlob(ctx context.Context, blobID string) ([]byte, *BlobManifest, error) {
	manifest, err := s.meta.GetManifest(ctx, blobID)
	if err != nil {
		return nil, nil, err
	}
	data, _, err := s.blobs.Get(ctx, blobID)
	if err != nil {
		return nil, nil, err
	}
	if manifest.Descriptor.Encryption != nil {
		data, err = decryptBytes(ctx, data, manifest.Descriptor.Encryption, s.resolveKey)
		if err != nil {
			return nil, nil, err
		}
	}
	return data, manifest, nil
}
