package binary

import "context"

// BlobStore stores and retrieves finalized blob payloads.
type BlobStore interface {
	Put(ctx context.Context, blobID string, data []byte, contentType string) (*BlobDescriptor, error)
	Get(ctx context.Context, blobID string) ([]byte, *BlobDescriptor, error)
	Head(ctx context.Context, blobID string) (*BlobDescriptor, error)
	Delete(ctx context.Context, blobID string) error
}

// ChunkStore supports chunked append/read and finalization for resumable transfer.
type ChunkStore interface {
	AppendChunk(ctx context.Context, key string, index int, data []byte) error
	ReadChunk(ctx context.Context, key string, index int) ([]byte, error)
	ListChunkCount(ctx context.Context, key string) (int, error)
	DeleteChunks(ctx context.Context, key string) error
}

// MetadataStore persists manifests, FHIR links, and sync status.
type MetadataStore interface {
	PutManifest(ctx context.Context, manifest BlobManifest) error
	GetManifest(ctx context.Context, blobID string) (*BlobManifest, error)
	GetManifestByHash(ctx context.Context, sha256 string) (*BlobManifest, error)
	DeleteManifest(ctx context.Context, blobID string) error

	PutBinaryLink(ctx context.Context, link BinaryLink) error
	GetBinaryLink(ctx context.Context, resourceID string) (*BinaryLink, error)
	DeleteBinaryLink(ctx context.Context, resourceID string) error

	PutDocumentLink(ctx context.Context, link DocumentAttachmentLink) error
	GetDocumentLink(ctx context.Context, documentID string, contentIndex int) (*DocumentAttachmentLink, error)
	DeleteDocumentLink(ctx context.Context, documentID string, contentIndex int) error

	PutSyncStatus(ctx context.Context, blobID string, status BlobSyncStatus) error
	GetSyncStatus(ctx context.Context, blobID string) (BlobSyncStatus, error)
	DeleteSyncStatus(ctx context.Context, blobID string) error
}

// TransferStore persists upload and download session state.
type TransferStore interface {
	CreateUploadSession(ctx context.Context, session UploadSession) error
	GetUploadSession(ctx context.Context, id string) (*UploadSession, error)
	UpdateUploadSession(ctx context.Context, session UploadSession) error
	DeleteUploadSession(ctx context.Context, id string) error

	CreateDownloadSession(ctx context.Context, session DownloadSession) error
	GetDownloadSession(ctx context.Context, id string) (*DownloadSession, error)
	UpdateDownloadSession(ctx context.Context, session DownloadSession) error
	DeleteDownloadSession(ctx context.Context, id string) error
}
