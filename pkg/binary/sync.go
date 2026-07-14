package binary

import (
	"context"
	"time"
)

// ChunkSyncService exports and imports blob content in chunks for sync pipelines.
type ChunkSyncService struct {
	transfers *TransferService
}

// NewChunkSyncService creates a chunk-based blob sync helper.
func NewChunkSyncService(transfers *TransferService) (*ChunkSyncService, error) {
	if transfers == nil {
		return nil, ErrInvalidArgument
	}
	return &ChunkSyncService{transfers: transfers}, nil
}

// ExportPlan describes how to stream a blob over chunk sync.
func (s *ChunkSyncService) ExportPlan(ctx context.Context, blobID string) (*ChunkSyncPlan, error) {
	manifest, err := s.transfers.meta.GetManifest(ctx, blobID)
	if err != nil {
		return nil, err
	}
	chunkSize := manifest.ChunkSize
	if chunkSize <= 0 {
		chunkSize = s.transfers.chunkSize
	}
	chunkCount := manifest.ChunkCount
	if chunkCount <= 0 {
		chunkCount = 1
	}
	return &ChunkSyncPlan{
		BlobID:      manifest.Descriptor.BlobID,
		SHA256:      manifest.Descriptor.SHA256,
		Size:        manifest.Descriptor.Size,
		ChunkSize:   chunkSize,
		ChunkCount:  chunkCount,
		ContentType: manifest.Descriptor.ContentType,
		CreatedAt:   manifest.CreatedAt,
	}, nil
}

// ExportChunk returns one plaintext sync chunk for a finalized blob.
func (s *ChunkSyncService) ExportChunk(ctx context.Context, blobID string, index int) (*SyncChunk, error) {
	data, manifest, err := s.transfers.ReadBlob(ctx, blobID)
	if err != nil {
		return nil, err
	}
	chunkSize := manifest.ChunkSize
	if chunkSize <= 0 {
		chunkSize = s.transfers.chunkSize
	}
	start := int64(index) * chunkSize
	if start >= int64(len(data)) {
		return nil, ErrNotFound
	}
	end := start + chunkSize
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	chunkCount := manifest.ChunkCount
	if chunkCount <= 0 {
		chunkCount = 1
	}
	return &SyncChunk{
		BlobID:      blobID,
		Index:       index,
		TotalChunks: chunkCount,
		Data:        append([]byte(nil), data[start:end]...),
	}, nil
}

// StartImport creates an inbound chunk-sync upload session.
func (s *ChunkSyncService) StartImport(ctx context.Context, plan ChunkSyncPlan, encryptionKeyID string, retention *BlobRetention) (*UploadSession, error) {
	return s.transfers.StartUploadWithOptions(ctx, UploadRequest{
		BlobID:          plan.BlobID,
		Size:            plan.Size,
		ContentType:     plan.ContentType,
		ExpectedChunks:  plan.ChunkCount,
		EncryptionKeyID: encryptionKeyID,
		Retention:       retention,
	})
}

// ApplyChunk appends one inbound sync chunk to the upload session.
func (s *ChunkSyncService) ApplyChunk(ctx context.Context, sessionID string, chunk SyncChunk) (*UploadSession, error) {
	return s.transfers.UploadChunk(ctx, sessionID, chunk.Index, chunk.Data)
}

// FinalizeImport finalizes an inbound chunk-sync upload.
func (s *ChunkSyncService) FinalizeImport(ctx context.Context, sessionID string) (*BlobManifest, error) {
	return s.transfers.FinalizeUpload(ctx, sessionID)
}

// LifecycleService enforces blob retention before deletion.
type LifecycleService struct {
	blobs BlobStore
	meta  MetadataStore
}

// NewLifecycleService creates retention-aware blob lifecycle operations.
func NewLifecycleService(blobs BlobStore, meta MetadataStore) (*LifecycleService, error) {
	if blobs == nil || meta == nil {
		return nil, ErrInvalidArgument
	}
	return &LifecycleService{blobs: blobs, meta: meta}, nil
}

// DeleteBlob deletes a blob only when its retention window has expired.
func (s *LifecycleService) DeleteBlob(ctx context.Context, blobID string, now time.Time) error {
	manifest, err := s.meta.GetManifest(ctx, blobID)
	if err != nil {
		return err
	}
	if manifest.Descriptor.Retention != nil && manifest.Descriptor.Retention.Mode != RetentionNone &&
		now.Before(manifest.Descriptor.Retention.RetainUntil) {
		return ErrRetentionLocked
	}
	if err := s.blobs.Delete(ctx, blobID); err != nil {
		return err
	}
	return s.meta.DeleteManifest(ctx, blobID)
}
