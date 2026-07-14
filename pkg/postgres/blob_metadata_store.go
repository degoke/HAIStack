package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/binary"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BlobMetadataStore persists blob manifests, links, sync status, and transfer sessions.
type BlobMetadataStore struct {
	exec     querier
	tenantID string
}

func newBlobMetadataStore(pool *pgxpool.Pool, tenantID string) *BlobMetadataStore {
	return &BlobMetadataStore{exec: pool, tenantID: tenantID}
}

func newBlobMetadataStoreTx(tx querier, tenantID string) *BlobMetadataStore {
	return &BlobMetadataStore{exec: tx, tenantID: tenantID}
}

func (s *BlobMetadataStore) PutManifest(ctx context.Context, manifest binary.BlobManifest) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO blob_manifest (
			tenant_id, blob_id, sha256, size, content_type, backend_kind, storage_ref,
			chunk_size, chunk_count, created_at, finalized_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (tenant_id, blob_id) DO UPDATE SET
			sha256 = EXCLUDED.sha256,
			size = EXCLUDED.size,
			content_type = EXCLUDED.content_type,
			backend_kind = EXCLUDED.backend_kind,
			storage_ref = EXCLUDED.storage_ref,
			chunk_size = EXCLUDED.chunk_size,
			chunk_count = EXCLUDED.chunk_count,
			created_at = EXCLUDED.created_at,
			finalized_at = EXCLUDED.finalized_at`,
		s.tenantID,
		manifest.Descriptor.BlobID,
		manifest.Descriptor.SHA256,
		manifest.Descriptor.Size,
		nullString(manifest.Descriptor.ContentType),
		string(manifest.Descriptor.Backend),
		manifest.Descriptor.Pointer.Ref,
		nullInt64(manifest.ChunkSize),
		manifest.ChunkCount,
		manifest.CreatedAt,
		nullTimePtr(manifest.FinalizedAt),
	)
	if err != nil {
		return fmt.Errorf("put blob manifest: %w", err)
	}
	return nil
}

func (s *BlobMetadataStore) GetManifest(ctx context.Context, blobID string) (*binary.BlobManifest, error) {
	return s.scanManifest(ctx, `
		SELECT blob_id, sha256, size, content_type, backend_kind, storage_ref,
			chunk_size, chunk_count, created_at, finalized_at
		FROM blob_manifest WHERE tenant_id = $1 AND blob_id = $2`, blobID)
}

func (s *BlobMetadataStore) GetManifestByHash(ctx context.Context, sha256 string) (*binary.BlobManifest, error) {
	return s.scanManifest(ctx, `
		SELECT blob_id, sha256, size, content_type, backend_kind, storage_ref,
			chunk_size, chunk_count, created_at, finalized_at
		FROM blob_manifest WHERE tenant_id = $1 AND sha256 = $2 LIMIT 1`, sha256)
}

func (s *BlobMetadataStore) DeleteManifest(ctx context.Context, blobID string) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM blob_manifest WHERE tenant_id = $1 AND blob_id = $2`,
		s.tenantID, blobID,
	)
	if err != nil {
		return fmt.Errorf("delete blob manifest: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) PutBinaryLink(ctx context.Context, link binary.BinaryLink) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO blob_binary_link (tenant_id, resource_id, blob_id, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, resource_id) DO UPDATE SET
			blob_id = EXCLUDED.blob_id,
			created_at = EXCLUDED.created_at`,
		s.tenantID, link.ResourceID, link.BlobID, link.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("put binary link: %w", err)
	}
	return nil
}

func (s *BlobMetadataStore) GetBinaryLink(ctx context.Context, resourceID string) (*binary.BinaryLink, error) {
	var link binary.BinaryLink
	err := s.exec.QueryRow(ctx, `
		SELECT resource_id, blob_id, created_at
		FROM blob_binary_link WHERE tenant_id = $1 AND resource_id = $2`,
		s.tenantID, resourceID,
	).Scan(&link.ResourceID, &link.BlobID, &link.CreatedAt)
	if isNoRows(err) {
		return nil, binary.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get binary link: %w", err)
	}
	return &link, nil
}

func (s *BlobMetadataStore) DeleteBinaryLink(ctx context.Context, resourceID string) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM blob_binary_link WHERE tenant_id = $1 AND resource_id = $2`,
		s.tenantID, resourceID,
	)
	if err != nil {
		return fmt.Errorf("delete binary link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) PutDocumentLink(ctx context.Context, link binary.DocumentAttachmentLink) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO blob_document_link (tenant_id, document_id, content_index, blob_id, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, document_id, content_index) DO UPDATE SET
			blob_id = EXCLUDED.blob_id,
			created_at = EXCLUDED.created_at`,
		s.tenantID, link.DocumentID, link.ContentIndex, link.BlobID, link.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("put document link: %w", err)
	}
	return nil
}

func (s *BlobMetadataStore) GetDocumentLink(ctx context.Context, documentID string, contentIndex int) (*binary.DocumentAttachmentLink, error) {
	var link binary.DocumentAttachmentLink
	err := s.exec.QueryRow(ctx, `
		SELECT document_id, content_index, blob_id, created_at
		FROM blob_document_link
		WHERE tenant_id = $1 AND document_id = $2 AND content_index = $3`,
		s.tenantID, documentID, contentIndex,
	).Scan(&link.DocumentID, &link.ContentIndex, &link.BlobID, &link.CreatedAt)
	if isNoRows(err) {
		return nil, binary.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get document link: %w", err)
	}
	return &link, nil
}

func (s *BlobMetadataStore) DeleteDocumentLink(ctx context.Context, documentID string, contentIndex int) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM blob_document_link
		WHERE tenant_id = $1 AND document_id = $2 AND content_index = $3`,
		s.tenantID, documentID, contentIndex,
	)
	if err != nil {
		return fmt.Errorf("delete document link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) PutSyncStatus(ctx context.Context, blobID string, status binary.BlobSyncStatus) error {
	now := time.Now().UTC()
	_, err := s.exec.Exec(ctx, `
		INSERT INTO blob_sync_status (tenant_id, blob_id, status, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, blob_id) DO UPDATE SET
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at`,
		s.tenantID, blobID, string(status), now,
	)
	if err != nil {
		return fmt.Errorf("put sync status: %w", err)
	}
	return nil
}

func (s *BlobMetadataStore) GetSyncStatus(ctx context.Context, blobID string) (binary.BlobSyncStatus, error) {
	var status string
	err := s.exec.QueryRow(ctx, `
		SELECT status FROM blob_sync_status WHERE tenant_id = $1 AND blob_id = $2`,
		s.tenantID, blobID,
	).Scan(&status)
	if isNoRows(err) {
		return "", binary.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get sync status: %w", err)
	}
	return binary.BlobSyncStatus(status), nil
}

func (s *BlobMetadataStore) DeleteSyncStatus(ctx context.Context, blobID string) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM blob_sync_status WHERE tenant_id = $1 AND blob_id = $2`,
		s.tenantID, blobID,
	)
	if err != nil {
		return fmt.Errorf("delete sync status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) CreateUploadSession(ctx context.Context, session binary.UploadSession) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO blob_transfer_session (
			tenant_id, session_id, session_kind, blob_id, sha256, size, content_type,
			chunk_size, transferred_bytes, transferred_chunks, expected_chunks,
			status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		s.tenantID, session.ID, string(binary.TransferUpload), session.BlobID,
		nullString(session.SHA256), session.Size, nullString(session.ContentType),
		session.ChunkSize, session.UploadedBytes, session.UploadedChunks,
		nullInt32(session.ExpectedChunks), string(session.Status),
		session.CreatedAt, session.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create upload session: %w", err)
	}
	return nil
}

func (s *BlobMetadataStore) GetUploadSession(ctx context.Context, id string) (*binary.UploadSession, error) {
	var session binary.UploadSession
	var sha256, contentType *string
	var expectedChunks *int32
	err := s.exec.QueryRow(ctx, `
		SELECT session_id, blob_id, sha256, size, content_type, chunk_size,
			transferred_bytes, transferred_chunks, expected_chunks, status, created_at, updated_at
		FROM blob_transfer_session
		WHERE tenant_id = $1 AND session_id = $2 AND session_kind = $3`,
		s.tenantID, id, string(binary.TransferUpload),
	).Scan(
		&session.ID, &session.BlobID, &sha256, &session.Size, &contentType,
		&session.ChunkSize, &session.UploadedBytes, &session.UploadedChunks,
		&expectedChunks, &session.Status, &session.CreatedAt, &session.UpdatedAt,
	)
	if isNoRows(err) {
		return nil, binary.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get upload session: %w", err)
	}
	if sha256 != nil {
		session.SHA256 = *sha256
	}
	if contentType != nil {
		session.ContentType = *contentType
	}
	if expectedChunks != nil {
		session.ExpectedChunks = int(*expectedChunks)
	}
	return &session, nil
}

func (s *BlobMetadataStore) UpdateUploadSession(ctx context.Context, session binary.UploadSession) error {
	tag, err := s.exec.Exec(ctx, `
		UPDATE blob_transfer_session SET
			blob_id = $3, sha256 = $4, size = $5, content_type = $6,
			chunk_size = $7, transferred_bytes = $8, transferred_chunks = $9,
			expected_chunks = $10, status = $11, updated_at = $12
		WHERE tenant_id = $1 AND session_id = $2 AND session_kind = $13`,
		s.tenantID, session.ID, session.BlobID, nullString(session.SHA256),
		session.Size, nullString(session.ContentType), session.ChunkSize,
		session.UploadedBytes, session.UploadedChunks, nullInt32(session.ExpectedChunks),
		string(session.Status), session.UpdatedAt, string(binary.TransferUpload),
	)
	if err != nil {
		return fmt.Errorf("update upload session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) DeleteUploadSession(ctx context.Context, id string) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM blob_transfer_session
		WHERE tenant_id = $1 AND session_id = $2 AND session_kind = $3`,
		s.tenantID, id, string(binary.TransferUpload),
	)
	if err != nil {
		return fmt.Errorf("delete upload session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) CreateDownloadSession(ctx context.Context, session binary.DownloadSession) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO blob_transfer_session (
			tenant_id, session_id, session_kind, blob_id, chunk_size,
			transferred_bytes, transferred_chunks, total_chunks,
			status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		s.tenantID, session.ID, string(binary.TransferDownload), session.BlobID,
		session.ChunkSize, session.DownloadedBytes, session.DownloadedChunks,
		nullInt32(session.TotalChunks), string(session.Status),
		session.CreatedAt, session.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create download session: %w", err)
	}
	return nil
}

func (s *BlobMetadataStore) GetDownloadSession(ctx context.Context, id string) (*binary.DownloadSession, error) {
	var session binary.DownloadSession
	var totalChunks *int32
	err := s.exec.QueryRow(ctx, `
		SELECT session_id, blob_id, chunk_size, transferred_bytes, transferred_chunks,
			total_chunks, status, created_at, updated_at
		FROM blob_transfer_session
		WHERE tenant_id = $1 AND session_id = $2 AND session_kind = $3`,
		s.tenantID, id, string(binary.TransferDownload),
	).Scan(
		&session.ID, &session.BlobID, &session.ChunkSize,
		&session.DownloadedBytes, &session.DownloadedChunks,
		&totalChunks, &session.Status, &session.CreatedAt, &session.UpdatedAt,
	)
	if isNoRows(err) {
		return nil, binary.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get download session: %w", err)
	}
	if totalChunks != nil {
		session.TotalChunks = int(*totalChunks)
	}
	return &session, nil
}

func (s *BlobMetadataStore) UpdateDownloadSession(ctx context.Context, session binary.DownloadSession) error {
	tag, err := s.exec.Exec(ctx, `
		UPDATE blob_transfer_session SET
			blob_id = $3, chunk_size = $4, transferred_bytes = $5, transferred_chunks = $6,
			total_chunks = $7, status = $8, updated_at = $9
		WHERE tenant_id = $1 AND session_id = $2 AND session_kind = $10`,
		s.tenantID, session.ID, session.BlobID, session.ChunkSize,
		session.DownloadedBytes, session.DownloadedChunks, nullInt32(session.TotalChunks),
		string(session.Status), session.UpdatedAt, string(binary.TransferDownload),
	)
	if err != nil {
		return fmt.Errorf("update download session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) DeleteDownloadSession(ctx context.Context, id string) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM blob_transfer_session
		WHERE tenant_id = $1 AND session_id = $2 AND session_kind = $3`,
		s.tenantID, id, string(binary.TransferDownload),
	)
	if err != nil {
		return fmt.Errorf("delete download session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) scanManifest(ctx context.Context, query string, arg string) (*binary.BlobManifest, error) {
	var (
		m           binary.BlobManifest
		contentType *string
		chunkSize   *int64
		backendKind string
		storageRef  string
		finalizedAt *time.Time
	)
	err := s.exec.QueryRow(ctx, query, s.tenantID, arg).Scan(
		&m.Descriptor.BlobID, &m.Descriptor.SHA256, &m.Descriptor.Size,
		&contentType, &backendKind, &storageRef,
		&chunkSize, &m.ChunkCount, &m.CreatedAt, &finalizedAt,
	)
	if isNoRows(err) {
		return nil, binary.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan blob manifest: %w", err)
	}
	if contentType != nil {
		m.Descriptor.ContentType = *contentType
	}
	m.Descriptor.Backend = binary.BackendKind(backendKind)
	m.Descriptor.Pointer = binary.StoragePointer{
		Backend: binary.BackendKind(backendKind),
		Ref:     storageRef,
	}
	if chunkSize != nil {
		m.ChunkSize = *chunkSize
	}
	m.FinalizedAt = finalizedAt
	return &m, nil
}

func nullInt64(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func nullInt32(v int) *int32 {
	if v == 0 {
		return nil
	}
	i := int32(v)
	return &i
}

func nullTimePtr(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	return t
}
