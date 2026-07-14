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
)

// StoragePointer is opaque backend location metadata serializable into resource
// metadata and sync payloads.
type StoragePointer struct {
	Backend BackendKind `json:"backend"`
	Ref     string      `json:"ref"`
}

// BlobDescriptor is stable blob identity and metadata without payload bytes.
type BlobDescriptor struct {
	BlobID      string         `json:"blobId"`
	SHA256      string         `json:"sha256"`
	Size        int64          `json:"size"`
	ContentType string         `json:"contentType,omitempty"`
	Backend     BackendKind    `json:"backend"`
	Pointer     StoragePointer `json:"pointer"`
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
	ID               string         `json:"id"`
	BlobID           string         `json:"blobId"`
	SHA256           string         `json:"sha256,omitempty"`
	Size             int64          `json:"size"`
	ContentType      string         `json:"contentType,omitempty"`
	ChunkSize        int64          `json:"chunkSize"`
	UploadedBytes    int64          `json:"uploadedBytes"`
	UploadedChunks   int            `json:"uploadedChunks"`
	ExpectedChunks   int            `json:"expectedChunks"`
	Status           BlobSyncStatus `json:"status"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
}

// DownloadSession holds resumable download state.
type DownloadSession struct {
	ID                string         `json:"id"`
	BlobID            string         `json:"blobId"`
	ChunkSize         int64          `json:"chunkSize"`
	DownloadedBytes   int64          `json:"downloadedBytes"`
	DownloadedChunks  int            `json:"downloadedChunks"`
	TotalChunks       int            `json:"totalChunks"`
	Status            BlobSyncStatus `json:"status"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
}

// BlobReference is metadata embedded in FHIR resources without payload bytes.
type BlobReference struct {
	BlobID      string         `json:"blobId"`
	SHA256      string         `json:"sha256"`
	Size        int64          `json:"size"`
	ContentType string         `json:"contentType,omitempty"`
	Pointer     StoragePointer `json:"pointer"`
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
