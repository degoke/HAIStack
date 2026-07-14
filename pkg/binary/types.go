package binary

import (
	"encoding/json"
	"time"
)

// BackendKind identifies the storage backend for a blob.
type BackendKind string

const (
	BackendLocalFile BackendKind = "local_file"
	BackendSQLite    BackendKind = "sqlite"
	BackendPostgres  BackendKind = "postgres"
	BackendS3        BackendKind = "s3"
)

// StoragePointer is opaque backend location metadata serializable into resource
// metadata and sync payloads.
type StoragePointer struct {
	Backend BackendKind `json:"backend"`
	Ref     string      `json:"ref"`
}

// EncryptionAlgorithm identifies at-rest encryption applied to stored blob bytes.
type EncryptionAlgorithm string

const (
	EncryptionNone      EncryptionAlgorithm = ""
	EncryptionAES256GCM EncryptionAlgorithm = "aes256-gcm"
)

// BlobEncryption captures per-blob encryption metadata.
type BlobEncryption struct {
	Algorithm EncryptionAlgorithm `json:"algorithm"`
	KeyID     string              `json:"keyId,omitempty"`
	Nonce     string              `json:"nonce,omitempty"`
}

// RetentionMode identifies blob retention behavior.
type RetentionMode string

const (
	RetentionNone       RetentionMode = ""
	RetentionGovernance RetentionMode = "governance"
	RetentionCompliance RetentionMode = "compliance"
)

// BlobRetention captures per-blob retention policy metadata.
type BlobRetention struct {
	Mode        RetentionMode `json:"mode"`
	RetainUntil time.Time     `json:"retainUntil"`
}

// BlobDescriptor is stable blob identity and metadata without payload bytes.
type BlobDescriptor struct {
	BlobID      string          `json:"blobId"`
	SHA256      string          `json:"sha256"`
	Size        int64           `json:"size"`
	ContentType string          `json:"contentType,omitempty"`
	Backend     BackendKind     `json:"backend"`
	Pointer     StoragePointer  `json:"pointer"`
	Encryption  *BlobEncryption `json:"encryption,omitempty"`
	Retention   *BlobRetention  `json:"retention,omitempty"`
}

// BlobManifest is a descriptor plus chunking and creation timestamps.
type BlobManifest struct {
	Descriptor  BlobDescriptor `json:"descriptor"`
	ChunkSize   int64          `json:"chunkSize,omitempty"`
	ChunkCount  int            `json:"chunkCount"`
	CreatedAt   time.Time      `json:"createdAt"`
	FinalizedAt *time.Time     `json:"finalizedAt,omitempty"`
}

// BinaryLink maps a FHIR Binary resource to a blob descriptor.
type BinaryLink struct {
	ResourceID string    `json:"resourceId"`
	BlobID     string    `json:"blobId"`
	CreatedAt  time.Time `json:"createdAt"`
}

// DocumentAttachmentLink maps a DocumentReference.content[].attachment entry to a blob.
type DocumentAttachmentLink struct {
	DocumentID   string    `json:"documentId"`
	ContentIndex int       `json:"contentIndex"`
	BlobID       string    `json:"blobId"`
	CreatedAt    time.Time `json:"createdAt"`
}

// BlobSyncStatus tracks blob transfer progress.
type BlobSyncStatus string

const (
	SyncPending     BlobSyncStatus = "pending"
	SyncUploading   BlobSyncStatus = "uploading"
	SyncUploaded    BlobSyncStatus = "uploaded"
	SyncDownloading BlobSyncStatus = "downloading"
	SyncComplete    BlobSyncStatus = "complete"
	SyncFailed      BlobSyncStatus = "failed"
)

// TransferKind distinguishes upload and download sessions.
type TransferKind string

const (
	TransferUpload   TransferKind = "upload"
	TransferDownload TransferKind = "download"
)

// UploadSession holds resumable upload state.
type UploadSession struct {
	ID             string          `json:"id"`
	BlobID         string          `json:"blobId"`
	SHA256         string          `json:"sha256,omitempty"`
	Size           int64           `json:"size"`
	ContentType    string          `json:"contentType,omitempty"`
	ChunkSize      int64           `json:"chunkSize"`
	UploadedBytes  int64           `json:"uploadedBytes"`
	UploadedChunks int             `json:"uploadedChunks"`
	ExpectedChunks int             `json:"expectedChunks"`
	Encryption     *BlobEncryption `json:"encryption,omitempty"`
	Retention      *BlobRetention  `json:"retention,omitempty"`
	Status         BlobSyncStatus  `json:"status"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

// DownloadSession holds resumable download state.
type DownloadSession struct {
	ID               string         `json:"id"`
	BlobID           string         `json:"blobId"`
	ChunkSize        int64          `json:"chunkSize"`
	DownloadedBytes  int64          `json:"downloadedBytes"`
	DownloadedChunks int            `json:"downloadedChunks"`
	TotalChunks      int            `json:"totalChunks"`
	Status           BlobSyncStatus `json:"status"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
}

// BlobReference is metadata embedded in FHIR resources without payload bytes.
type BlobReference struct {
	BlobID      string         `json:"blobId"`
	SHA256      string         `json:"sha256"`
	Size        int64          `json:"size"`
	ContentType string         `json:"contentType,omitempty"`
	Pointer     StoragePointer `json:"pointer"`
}

// BlobWriteOptions configures how a blob is stored.
type BlobWriteOptions struct {
	ContentType string
	Retention   *BlobRetention
}

// UploadRequest begins a resumable upload with optional encryption and retention.
type UploadRequest struct {
	BlobID          string
	Size            int64
	ContentType     string
	ExpectedChunks  int
	EncryptionKeyID string
	Retention       *BlobRetention
}

// SignedAccessURL is a pre-signed object-storage request.
type SignedAccessURL struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	ExpiresAt time.Time         `json:"expiresAt"`
}

// ChunkSyncPlan describes chunk-based blob sync metadata.
type ChunkSyncPlan struct {
	BlobID      string    `json:"blobId"`
	SHA256      string    `json:"sha256"`
	Size        int64     `json:"size"`
	ChunkSize   int64     `json:"chunkSize"`
	ChunkCount  int       `json:"chunkCount"`
	ContentType string    `json:"contentType,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// SyncChunk is one chunk transferred over blob sync.
type SyncChunk struct {
	BlobID      string `json:"blobId"`
	Index       int    `json:"index"`
	TotalChunks int    `json:"totalChunks"`
	Data        []byte `json:"data"`
}

// MarshalPointerJSON serializes a storage pointer for embedding in resource JSON.
func MarshalPointerJSON(p StoragePointer) ([]byte, error) {
	return json.Marshal(p)
}

// UnmarshalPointerJSON deserializes a storage pointer from resource JSON.
func UnmarshalPointerJSON(data []byte) (StoragePointer, error) {
	var p StoragePointer
	if err := json.Unmarshal(data, &p); err != nil {
		return StoragePointer{}, err
	}
	return p, nil
}
