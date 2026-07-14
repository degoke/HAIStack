package binary_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/binary"
	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestHashSHA256StabilityAndDedup(t *testing.T) {
	payload := []byte("hello blob world")
	h1 := binary.HashSHA256(payload)
	h2 := binary.HashSHA256(payload)
	if h1 != h2 {
		t.Fatalf("hash mismatch: %s vs %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64-char hex digest, got %d", len(h1))
	}
	if binary.HashSHA256([]byte("other")) == h1 {
		t.Fatal("different payloads should not share hash")
	}
}

func TestLocalFileBlobStorePutGetHeadDelete(t *testing.T) {
	root := t.TempDir()
	files, err := binary.NewLocalFileBlobStore(root)
	if err != nil {
		t.Fatalf("NewLocalFileBlobStore: %v", err)
	}
	ctx := context.Background()
	data := []byte("local file payload")

	desc, err := files.Put(ctx, "blob-1", data, "text/plain")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if desc.SHA256 != binary.HashSHA256(data) {
		t.Fatalf("unexpected hash: %s", desc.SHA256)
	}

	got, err := files.GetByHash(ctx, desc.SHA256)
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("GetByHash = %q, want %q", got, data)
	}

	head, err := files.Head(ctx, "blob-1", desc.SHA256, "text/plain")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Size != int64(len(data)) {
		t.Fatalf("Head size = %d", head.Size)
	}

	if err := files.Delete(ctx, desc.SHA256); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := files.GetByHash(ctx, desc.SHA256); !errors.Is(err, binary.ErrNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestLocalFileChunkedUploadResumeAndFinalize(t *testing.T) {
	root := t.TempDir()
	files, err := binary.NewLocalFileBlobStore(root)
	if err != nil {
		t.Fatalf("NewLocalFileBlobStore: %v", err)
	}
	ctx := context.Background()
	payload := []byte("chunk-one-chunk-two-final")
	chunkA := payload[:10]
	chunkB := payload[10:]

	key := "upload:test-session"
	if err := files.AppendChunk(ctx, key, 0, chunkA); err != nil {
		t.Fatalf("AppendChunk 0: %v", err)
	}
	// Simulate interruption — only first chunk present.
	count, err := files.ListChunkCount(ctx, key)
	if err != nil || count != 1 {
		t.Fatalf("ListChunkCount after first chunk = %d, %v", count, err)
	}

	if err := files.AppendChunk(ctx, key, 1, chunkB); err != nil {
		t.Fatalf("AppendChunk 1: %v", err)
	}

	manifest, err := files.FinalizeChunks(ctx, key, "blob-final", "application/octet-stream")
	if err != nil {
		t.Fatalf("FinalizeChunks: %v", err)
	}
	if manifest.Descriptor.SHA256 != binary.HashSHA256(payload) {
		t.Fatalf("unexpected finalized hash: %s", manifest.Descriptor.SHA256)
	}

	got, err := files.GetByHash(ctx, manifest.Descriptor.SHA256)
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("final payload mismatch")
	}
}

func TestTransferServiceUploadDownloadAndSyncStatus(t *testing.T) {
	ctx := context.Background()
	blobs := newMemBlobStore()
	chunks := newMemChunkStore()
	meta := newMemMetadataStore()
	transfers := newMemTransferStore()

	svc, err := binary.NewTransferService(binary.TransferConfig{
		Blobs: blobs, Chunks: chunks, Metadata: meta, Transfers: transfers, ChunkSize: 8,
	})
	if err != nil {
		t.Fatalf("NewTransferService: %v", err)
	}

	payload := []byte("0123456789abcdef")
	upload, err := svc.StartUpload(ctx, "blob-x", int64(len(payload)), "text/plain", 2)
	if err != nil {
		t.Fatalf("StartUpload: %v", err)
	}
	status, err := meta.GetSyncStatus(ctx, "blob-x")
	if err != nil || status != binary.SyncUploading {
		t.Fatalf("sync status after start = %q, %v", status, err)
	}

	if _, err := svc.UploadChunk(ctx, upload.ID, 0, payload[:8]); err != nil {
		t.Fatalf("UploadChunk 0: %v", err)
	}
	if _, err := svc.UploadChunk(ctx, upload.ID, 1, payload[8:]); err != nil {
		t.Fatalf("UploadChunk 1: %v", err)
	}

	manifest, err := svc.FinalizeUpload(ctx, upload.ID)
	if err != nil {
		t.Fatalf("FinalizeUpload: %v", err)
	}
	if manifest.Descriptor.SHA256 != binary.HashSHA256(payload) {
		t.Fatalf("unexpected hash: %s", manifest.Descriptor.SHA256)
	}
	status, err = meta.GetSyncStatus(ctx, "blob-x")
	if err != nil || status != binary.SyncComplete {
		t.Fatalf("sync status after finalize = %q, %v", status, err)
	}

	download, err := svc.StartDownload(ctx, "blob-x")
	if err != nil {
		t.Fatalf("StartDownload: %v", err)
	}
	chunk0, session, err := svc.DownloadChunk(ctx, download.ID, 0)
	if err != nil {
		t.Fatalf("DownloadChunk 0: %v", err)
	}
	chunk1, session, err := svc.DownloadChunk(ctx, download.ID, 1)
	if err != nil {
		t.Fatalf("DownloadChunk 1: %v", err)
	}
	if session.Status != binary.SyncComplete {
		t.Fatalf("download session status = %q", session.Status)
	}
	if !bytes.Equal(append(chunk0, chunk1...), payload) {
		t.Fatal("downloaded payload mismatch")
	}
}

func TestTransferServiceDedupByHash(t *testing.T) {
	ctx := context.Background()
	blobs := newMemBlobStore()
	chunks := newMemChunkStore()
	meta := newMemMetadataStore()
	transfers := newMemTransferStore()

	svc, err := binary.NewTransferService(binary.TransferConfig{
		Blobs: blobs, Chunks: chunks, Metadata: meta, Transfers: transfers, ChunkSize: 64,
	})
	if err != nil {
		t.Fatalf("NewTransferService: %v", err)
	}

	payload := []byte("dedup-me")
	upload1, err := svc.StartUpload(ctx, "blob-a", int64(len(payload)), "text/plain", 1)
	if err != nil {
		t.Fatalf("StartUpload 1: %v", err)
	}
	if _, err := svc.UploadChunk(ctx, upload1.ID, 0, payload); err != nil {
		t.Fatalf("UploadChunk: %v", err)
	}
	manifest1, err := svc.FinalizeUpload(ctx, upload1.ID)
	if err != nil {
		t.Fatalf("FinalizeUpload 1: %v", err)
	}

	upload2, err := svc.StartUpload(ctx, "blob-b", int64(len(payload)), "text/plain", 1)
	if err != nil {
		t.Fatalf("StartUpload 2: %v", err)
	}
	if _, err := svc.UploadChunk(ctx, upload2.ID, 0, payload); err != nil {
		t.Fatalf("UploadChunk 2: %v", err)
	}
	manifest2, err := svc.FinalizeUpload(ctx, upload2.ID)
	if err != nil {
		t.Fatalf("FinalizeUpload 2: %v", err)
	}
	if manifest1.Descriptor.SHA256 != manifest2.Descriptor.SHA256 {
		t.Fatal("expected dedup to return same hash")
	}
}

func TestBinaryAndDocumentLinks(t *testing.T) {
	ctx := context.Background()
	meta := newMemMetadataStore()
	links := binary.NewLinkService(meta)

	if err := links.LinkBinary(ctx, "bin-res-1", "blob-1"); err != nil {
		t.Fatalf("LinkBinary: %v", err)
	}
	got, err := links.GetBinaryLink(ctx, "bin-res-1")
	if err != nil || got.BlobID != "blob-1" {
		t.Fatalf("GetBinaryLink = %+v, %v", got, err)
	}
	if err := links.DeleteBinaryLink(ctx, "bin-res-1"); err != nil {
		t.Fatalf("DeleteBinaryLink: %v", err)
	}

	if err := links.LinkDocumentAttachment(ctx, "doc-1", 0, "blob-2"); err != nil {
		t.Fatalf("LinkDocumentAttachment: %v", err)
	}
	docLink, err := links.GetDocumentAttachmentLink(ctx, "doc-1", 0)
	if err != nil || docLink.BlobID != "blob-2" {
		t.Fatalf("GetDocumentAttachmentLink = %+v, %v", docLink, err)
	}
	if err := links.DeleteDocumentAttachmentLink(ctx, "doc-1", 0); err != nil {
		t.Fatalf("DeleteDocumentAttachmentLink: %v", err)
	}
}

func TestFHIRHelpersMetadataOnlyRoundTrip(t *testing.T) {
	ref := binary.BlobReference{
		BlobID: "blob-1", SHA256: "abc123", Size: 42, ContentType: "image/png",
		Pointer: binary.StoragePointer{Backend: binary.BackendLocalFile, Ref: "/data/blobs/abc"},
	}

	binJSON, err := binary.BuildBinaryMetadataJSON("bin-1", "image/png", ref)
	if err != nil {
		t.Fatalf("BuildBinaryMetadataJSON: %v", err)
	}
	if binary.ResourceHasPayloadBytes(binJSON) {
		t.Fatal("Binary metadata JSON should not contain payload bytes")
	}
	extracted, err := binary.ExtractBinaryBlobRef(binJSON)
	if err != nil {
		t.Fatalf("ExtractBinaryBlobRef: %v", err)
	}
	if extracted.BlobID != ref.BlobID || extracted.SHA256 != ref.SHA256 {
		t.Fatalf("ExtractBinaryBlobRef = %+v", extracted)
	}

	docJSON := []byte(`{"resourceType":"DocumentReference","id":"doc-1","content":[{}]}`)
	embedded, err := binary.EmbedDocumentAttachment(docJSON, 0, "application/pdf", ref)
	if err != nil {
		t.Fatalf("EmbedDocumentAttachment: %v", err)
	}
	if binary.ResourceHasPayloadBytes(embedded) {
		t.Fatal("DocumentReference JSON should not contain inline attachment data")
	}
	refs, err := binary.ExtractBlobReferences(embedded)
	if err != nil || len(refs) != 1 || refs[0].BlobID != "blob-1" {
		t.Fatalf("ExtractBlobReferences = %+v, %v", refs, err)
	}
}

func TestSQLiteChunkBlobStorePutGetHeadDelete(t *testing.T) {
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	blobStore := db.ChunkBlobStore()
	data := []byte("sqlite blob bytes")
	desc, err := blobStore.Put(ctx, "blob-sqlite-1", data, "text/plain")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if desc.SHA256 != binary.HashSHA256(data) {
		t.Fatalf("unexpected hash: %s", desc.SHA256)
	}

	got, head, err := blobStore.Get(ctx, "blob-sqlite-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, data) || head.Size != int64(len(data)) {
		t.Fatalf("Get mismatch: %d bytes", len(got))
	}

	meta, err := blobStore.Head(ctx, "blob-sqlite-1")
	if err != nil || meta.BlobID != "blob-sqlite-1" {
		t.Fatalf("Head = %+v, %v", meta, err)
	}

	if err := blobStore.Delete(ctx, "blob-sqlite-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := blobStore.Get(ctx, "blob-sqlite-1"); !errors.Is(err, binary.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	// Legacy binary_object still works.
	legacy := db.BinaryStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	if err := legacy.Put(ctx, store.BinaryObject{
		Key: "legacy-key", ContentType: "text/plain", Size: int64(len(data)),
		Hash: binary.HashSHA256(data), Data: data, CreatedAt: now,
	}); err != nil {
		t.Fatalf("legacy Put: %v", err)
	}
}

func TestSQLiteBlobMigrationAndMetadataStores(t *testing.T) {
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	tables := []string{
		"blob_manifest", "blob_chunk", "blob_binary_link",
		"blob_document_link", "blob_sync_status", "blob_transfer_session",
	}
	for _, table := range tables {
		var name string
		err := db.SQL().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %q missing: %v", table, err)
		}
	}

	meta := db.BlobMetadataStore()
	now := fixedTime()
	if err := meta.PutSyncStatus(ctx, "blob-1", binary.SyncPending); err != nil {
		t.Fatalf("PutSyncStatus: %v", err)
	}
	status, err := meta.GetSyncStatus(ctx, "blob-1")
	if err != nil || status != binary.SyncPending {
		t.Fatalf("GetSyncStatus = %q, %v", status, err)
	}
	if err := meta.PutBinaryLink(ctx, binary.BinaryLink{ResourceID: "bin-1", BlobID: "blob-1", CreatedAt: now}); err != nil {
		t.Fatalf("PutBinaryLink: %v", err)
	}
}

func TestPostgresChunkBlobStorePutGetHeadDelete(t *testing.T) {
	if os.Getenv("TEST_POSTGRES_DSN") == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	db, cleanup := openPostgresTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := postgresTestTenant(t, db, "blob-tenant")
	blobStore := tdb.ChunkBlobStore()

	data := []byte("postgres blob bytes")
	desc, err := blobStore.Put(ctx, "blob-pg-1", data, "text/plain")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if desc.SHA256 != binary.HashSHA256(data) {
		t.Fatalf("unexpected hash: %s", desc.SHA256)
	}

	got, _, err := blobStore.Get(ctx, "blob-pg-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("payload mismatch")
	}

	if err := blobStore.Delete(ctx, "blob-pg-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Legacy binary_object still works.
	now := time.Now().UTC()
	if err := tdb.BinaryStore().Put(ctx, store.BinaryObject{
		Key: "legacy-pg", ContentType: "text/plain", Size: int64(len(data)),
		Hash: binary.HashSHA256(data), Data: data, CreatedAt: now,
	}); err != nil {
		t.Fatalf("legacy Put: %v", err)
	}
}

func TestPostgresBlobMetadataInSession(t *testing.T) {
	if os.Getenv("TEST_POSTGRES_DSN") == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	db, cleanup := openPostgresTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := postgresTestTenant(t, db, "blob-session")

	session, err := tdb.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	meta := session.BinaryMetadataStore()
	if err := meta.PutSyncStatus(ctx, "blob-sess", binary.SyncPending); err != nil {
		t.Fatalf("PutSyncStatus: %v", err)
	}
	if err := session.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	status, err := tdb.BlobMetadataStore().GetSyncStatus(ctx, "blob-sess")
	if err != nil || status != binary.SyncPending {
		t.Fatalf("GetSyncStatus = %q, %v", status, err)
	}
}

func TestLocalFileBlobStoreAdapterWithMetadata(t *testing.T) {
	root := t.TempDir()
	files, err := binary.NewLocalFileBlobStore(root)
	if err != nil {
		t.Fatalf("NewLocalFileBlobStore: %v", err)
	}
	meta := newMemMetadataStore()
	store := binary.NewLocalFileBlobStoreAdapter(files, meta)
	ctx := context.Background()

	data := []byte("adapter payload")
	desc, err := store.Put(ctx, "blob-adapt", data, "text/plain")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, head, err := store.Get(ctx, "blob-adapt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, data) || head.SHA256 != desc.SHA256 {
		t.Fatal("adapter get mismatch")
	}
}

// Helpers for postgres tests — thin wrappers to avoid importing postgres_test package.

func openPostgresTestDB(t *testing.T) (*postgres.DB, func()) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	db, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		db.Close()
		t.Fatalf("Migrate: %v", err)
	}
	return db, func() { db.Close() }
}

func postgresTestTenant(t *testing.T, db *postgres.DB, tenantID string) *postgres.TenantDB {
	t.Helper()
	return db.Tenant(tenantID)
}
