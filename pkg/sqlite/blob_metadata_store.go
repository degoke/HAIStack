package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/binary"
)

// BlobMetadataStore persists blob manifests, links, sync status, and transfer sessions.
type BlobMetadataStore struct {
	exec queryExec
}

func newBlobMetadataStore(db *sql.DB) *BlobMetadataStore {
	return &BlobMetadataStore{exec: db}
}

func newBlobMetadataStoreTx(tx *sql.Tx) *BlobMetadataStore {
	return &BlobMetadataStore{exec: tx}
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *BlobMetadataStore) PutManifest(ctx context.Context, manifest binary.BlobManifest) error {
	finalized := ""
	if manifest.FinalizedAt != nil {
		finalized = formatTime(*manifest.FinalizedAt)
	}
	encryptionAlgorithm, encryptionKeyID, encryptionNonce := "", "", ""
	if manifest.Descriptor.Encryption != nil {
		encryptionAlgorithm = string(manifest.Descriptor.Encryption.Algorithm)
		encryptionKeyID = manifest.Descriptor.Encryption.KeyID
		encryptionNonce = manifest.Descriptor.Encryption.Nonce
	}
	retentionMode, retainUntil := "", ""
	if manifest.Descriptor.Retention != nil {
		retentionMode = string(manifest.Descriptor.Retention.Mode)
		retainUntil = formatTime(manifest.Descriptor.Retention.RetainUntil)
	}
	_, err := s.exec.ExecContext(ctx, `
		INSERT INTO blob_manifest (
			blob_id, sha256, size, content_type, backend_kind, storage_ref,
			chunk_size, chunk_count, created_at, finalized_at,
			encryption_algorithm, encryption_key_id, encryption_nonce,
			retention_mode, retain_until
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(blob_id) DO UPDATE SET
			sha256 = excluded.sha256,
			size = excluded.size,
			content_type = excluded.content_type,
			backend_kind = excluded.backend_kind,
			storage_ref = excluded.storage_ref,
			chunk_size = excluded.chunk_size,
			chunk_count = excluded.chunk_count,
			created_at = excluded.created_at,
			finalized_at = excluded.finalized_at,
			encryption_algorithm = excluded.encryption_algorithm,
			encryption_key_id = excluded.encryption_key_id,
			encryption_nonce = excluded.encryption_nonce,
			retention_mode = excluded.retention_mode,
			retain_until = excluded.retain_until`,
		manifest.Descriptor.BlobID,
		manifest.Descriptor.SHA256,
		manifest.Descriptor.Size,
		nullableString(manifest.Descriptor.ContentType),
		string(manifest.Descriptor.Backend),
		manifest.Descriptor.Pointer.Ref,
		nullInt64(manifest.ChunkSize),
		manifest.ChunkCount,
		formatTime(manifest.CreatedAt),
		nullableString(finalized),
		nullableString(encryptionAlgorithm),
		nullableString(encryptionKeyID),
		nullableString(encryptionNonce),
		nullableString(retentionMode),
		nullableString(retainUntil),
	)
	if err != nil {
		return fmt.Errorf("put blob manifest: %w", err)
	}
	return nil
}

func (s *BlobMetadataStore) GetManifest(ctx context.Context, blobID string) (*binary.BlobManifest, error) {
	return s.scanManifest(ctx, `
		SELECT blob_id, sha256, size, content_type, backend_kind, storage_ref,
			chunk_size, chunk_count, created_at, finalized_at,
			encryption_algorithm, encryption_key_id, encryption_nonce,
			retention_mode, retain_until
		FROM blob_manifest WHERE blob_id = ?`, blobID)
}

func (s *BlobMetadataStore) GetManifestByHash(ctx context.Context, sha256 string) (*binary.BlobManifest, error) {
	return s.scanManifest(ctx, `
		SELECT blob_id, sha256, size, content_type, backend_kind, storage_ref,
			chunk_size, chunk_count, created_at, finalized_at,
			encryption_algorithm, encryption_key_id, encryption_nonce,
			retention_mode, retain_until
		FROM blob_manifest WHERE sha256 = ? LIMIT 1`, sha256)
}

func (s *BlobMetadataStore) DeleteManifest(ctx context.Context, blobID string) error {
	result, err := s.exec.ExecContext(ctx, `DELETE FROM blob_manifest WHERE blob_id = ?`, blobID)
	if err != nil {
		return fmt.Errorf("delete blob manifest: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete blob manifest rows: %w", err)
	}
	if rows == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) PutBinaryLink(ctx context.Context, link binary.BinaryLink) error {
	_, err := s.exec.ExecContext(ctx, `
		INSERT INTO blob_binary_link (resource_id, blob_id, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT(resource_id) DO UPDATE SET
			blob_id = excluded.blob_id,
			created_at = excluded.created_at`,
		link.ResourceID, link.BlobID, formatTime(link.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("put binary link: %w", err)
	}
	return nil
}

func (s *BlobMetadataStore) GetBinaryLink(ctx context.Context, resourceID string) (*binary.BinaryLink, error) {
	var link binary.BinaryLink
	var createdAt string
	err := s.exec.QueryRowContext(ctx, `
		SELECT resource_id, blob_id, created_at
		FROM blob_binary_link WHERE resource_id = ?`, resourceID,
	).Scan(&link.ResourceID, &link.BlobID, &createdAt)
	if err == sql.ErrNoRows {
		return nil, binary.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get binary link: %w", err)
	}
	ts, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	link.CreatedAt = ts
	return &link, nil
}

func (s *BlobMetadataStore) DeleteBinaryLink(ctx context.Context, resourceID string) error {
	result, err := s.exec.ExecContext(ctx, `DELETE FROM blob_binary_link WHERE resource_id = ?`, resourceID)
	if err != nil {
		return fmt.Errorf("delete binary link: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete binary link rows: %w", err)
	}
	if rows == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) PutDocumentLink(ctx context.Context, link binary.DocumentAttachmentLink) error {
	_, err := s.exec.ExecContext(ctx, `
		INSERT INTO blob_document_link (document_id, content_index, blob_id, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(document_id, content_index) DO UPDATE SET
			blob_id = excluded.blob_id,
			created_at = excluded.created_at`,
		link.DocumentID, link.ContentIndex, link.BlobID, formatTime(link.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("put document link: %w", err)
	}
	return nil
}

func (s *BlobMetadataStore) GetDocumentLink(ctx context.Context, documentID string, contentIndex int) (*binary.DocumentAttachmentLink, error) {
	var link binary.DocumentAttachmentLink
	var createdAt string
	err := s.exec.QueryRowContext(ctx, `
		SELECT document_id, content_index, blob_id, created_at
		FROM blob_document_link WHERE document_id = ? AND content_index = ?`,
		documentID, contentIndex,
	).Scan(&link.DocumentID, &link.ContentIndex, &link.BlobID, &createdAt)
	if err == sql.ErrNoRows {
		return nil, binary.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get document link: %w", err)
	}
	ts, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	link.CreatedAt = ts
	return &link, nil
}

func (s *BlobMetadataStore) DeleteDocumentLink(ctx context.Context, documentID string, contentIndex int) error {
	result, err := s.exec.ExecContext(ctx, `
		DELETE FROM blob_document_link WHERE document_id = ? AND content_index = ?`,
		documentID, contentIndex,
	)
	if err != nil {
		return fmt.Errorf("delete document link: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete document link rows: %w", err)
	}
	if rows == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) PutSyncStatus(ctx context.Context, blobID string, status binary.BlobSyncStatus) error {
	now := time.Now().UTC()
	_, err := s.exec.ExecContext(ctx, `
		INSERT INTO blob_sync_status (blob_id, status, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(blob_id) DO UPDATE SET
			status = excluded.status,
			updated_at = excluded.updated_at`,
		blobID, string(status), formatTime(now),
	)
	if err != nil {
		return fmt.Errorf("put sync status: %w", err)
	}
	return nil
}

func (s *BlobMetadataStore) GetSyncStatus(ctx context.Context, blobID string) (binary.BlobSyncStatus, error) {
	var status string
	err := s.exec.QueryRowContext(ctx, `
		SELECT status FROM blob_sync_status WHERE blob_id = ?`, blobID,
	).Scan(&status)
	if err == sql.ErrNoRows {
		return "", binary.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get sync status: %w", err)
	}
	return binary.BlobSyncStatus(status), nil
}

func (s *BlobMetadataStore) DeleteSyncStatus(ctx context.Context, blobID string) error {
	result, err := s.exec.ExecContext(ctx, `DELETE FROM blob_sync_status WHERE blob_id = ?`, blobID)
	if err != nil {
		return fmt.Errorf("delete sync status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete sync status rows: %w", err)
	}
	if rows == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) CreateUploadSession(ctx context.Context, session binary.UploadSession) error {
	_, err := s.exec.ExecContext(ctx, `
		INSERT INTO blob_transfer_session (
			session_id, session_kind, blob_id, sha256, size, content_type,
			chunk_size, transferred_bytes, transferred_chunks, expected_chunks,
			status, created_at, updated_at,
			encryption_algorithm, encryption_key_id, encryption_nonce,
			retention_mode, retain_until
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, string(binary.TransferUpload), session.BlobID,
		nullableString(session.SHA256), session.Size, nullableString(session.ContentType),
		session.ChunkSize, session.UploadedBytes, session.UploadedChunks,
		nullInt(session.ExpectedChunks), string(session.Status),
		formatTime(session.CreatedAt), formatTime(session.UpdatedAt),
		nullableString(uploadEncryptionAlgorithm(session)),
		nullableString(uploadEncryptionKeyID(session)),
		nullableString(uploadEncryptionNonce(session)),
		nullableString(uploadRetentionMode(session)),
		nullableString(uploadRetainUntil(session)),
	)
	if err != nil {
		return fmt.Errorf("create upload session: %w", err)
	}
	return nil
}

func (s *BlobMetadataStore) GetUploadSession(ctx context.Context, id string) (*binary.UploadSession, error) {
	var session binary.UploadSession
	var sha256, contentType sql.NullString
	var encAlgorithm, encKeyID, encNonce sql.NullString
	var retentionMode, retainUntil sql.NullString
	var expectedChunks sql.NullInt64
	var createdAt, updatedAt string
	err := s.exec.QueryRowContext(ctx, `
		SELECT session_id, blob_id, sha256, size, content_type, chunk_size,
			transferred_bytes, transferred_chunks, expected_chunks, status, created_at, updated_at,
			encryption_algorithm, encryption_key_id, encryption_nonce, retention_mode, retain_until
		FROM blob_transfer_session
		WHERE session_id = ? AND session_kind = ?`, id, string(binary.TransferUpload),
	).Scan(
		&session.ID, &session.BlobID, &sha256, &session.Size, &contentType,
		&session.ChunkSize, &session.UploadedBytes, &session.UploadedChunks,
		&expectedChunks, &session.Status, &createdAt, &updatedAt,
		&encAlgorithm, &encKeyID, &encNonce, &retentionMode, &retainUntil,
	)
	if err == sql.ErrNoRows {
		return nil, binary.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get upload session: %w", err)
	}
	if sha256.Valid {
		session.SHA256 = sha256.String
	}
	if contentType.Valid {
		session.ContentType = contentType.String
	}
	if expectedChunks.Valid {
		session.ExpectedChunks = int(expectedChunks.Int64)
	}
	applyUploadPolicyFields(&session, encAlgorithm, encKeyID, encNonce, retentionMode, retainUntil)
	ca, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	ua, err := parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	session.CreatedAt = ca
	session.UpdatedAt = ua
	return &session, nil
}

func (s *BlobMetadataStore) UpdateUploadSession(ctx context.Context, session binary.UploadSession) error {
	result, err := s.exec.ExecContext(ctx, `
		UPDATE blob_transfer_session SET
			blob_id = ?, sha256 = ?, size = ?, content_type = ?,
			chunk_size = ?, transferred_bytes = ?, transferred_chunks = ?,
			expected_chunks = ?, status = ?, updated_at = ?,
			encryption_algorithm = ?, encryption_key_id = ?, encryption_nonce = ?,
			retention_mode = ?, retain_until = ?
		WHERE session_id = ? AND session_kind = ?`,
		session.BlobID, nullableString(session.SHA256), session.Size, nullableString(session.ContentType),
		session.ChunkSize, session.UploadedBytes, session.UploadedChunks,
		nullInt(session.ExpectedChunks), string(session.Status), formatTime(session.UpdatedAt),
		nullableString(uploadEncryptionAlgorithm(session)),
		nullableString(uploadEncryptionKeyID(session)),
		nullableString(uploadEncryptionNonce(session)),
		nullableString(uploadRetentionMode(session)),
		nullableString(uploadRetainUntil(session)),
		session.ID, string(binary.TransferUpload),
	)
	if err != nil {
		return fmt.Errorf("update upload session: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update upload session rows: %w", err)
	}
	if rows == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) DeleteUploadSession(ctx context.Context, id string) error {
	result, err := s.exec.ExecContext(ctx, `
		DELETE FROM blob_transfer_session WHERE session_id = ? AND session_kind = ?`,
		id, string(binary.TransferUpload),
	)
	if err != nil {
		return fmt.Errorf("delete upload session: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete upload session rows: %w", err)
	}
	if rows == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) CreateDownloadSession(ctx context.Context, session binary.DownloadSession) error {
	_, err := s.exec.ExecContext(ctx, `
		INSERT INTO blob_transfer_session (
			session_id, session_kind, blob_id, chunk_size,
			transferred_bytes, transferred_chunks, total_chunks,
			status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, string(binary.TransferDownload), session.BlobID,
		session.ChunkSize, session.DownloadedBytes, session.DownloadedChunks,
		nullInt(session.TotalChunks), string(session.Status),
		formatTime(session.CreatedAt), formatTime(session.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("create download session: %w", err)
	}
	return nil
}

func (s *BlobMetadataStore) GetDownloadSession(ctx context.Context, id string) (*binary.DownloadSession, error) {
	var session binary.DownloadSession
	var totalChunks sql.NullInt64
	var createdAt, updatedAt string
	err := s.exec.QueryRowContext(ctx, `
		SELECT session_id, blob_id, chunk_size, transferred_bytes, transferred_chunks,
			total_chunks, status, created_at, updated_at
		FROM blob_transfer_session
		WHERE session_id = ? AND session_kind = ?`, id, string(binary.TransferDownload),
	).Scan(
		&session.ID, &session.BlobID, &session.ChunkSize,
		&session.DownloadedBytes, &session.DownloadedChunks,
		&totalChunks, &session.Status, &createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, binary.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get download session: %w", err)
	}
	if totalChunks.Valid {
		session.TotalChunks = int(totalChunks.Int64)
	}
	ca, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	ua, err := parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	session.CreatedAt = ca
	session.UpdatedAt = ua
	return &session, nil
}

func (s *BlobMetadataStore) UpdateDownloadSession(ctx context.Context, session binary.DownloadSession) error {
	result, err := s.exec.ExecContext(ctx, `
		UPDATE blob_transfer_session SET
			blob_id = ?, chunk_size = ?, transferred_bytes = ?, transferred_chunks = ?,
			total_chunks = ?, status = ?, updated_at = ?
		WHERE session_id = ? AND session_kind = ?`,
		session.BlobID, session.ChunkSize, session.DownloadedBytes, session.DownloadedChunks,
		nullInt(session.TotalChunks), string(session.Status), formatTime(session.UpdatedAt),
		session.ID, string(binary.TransferDownload),
	)
	if err != nil {
		return fmt.Errorf("update download session: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update download session rows: %w", err)
	}
	if rows == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) DeleteDownloadSession(ctx context.Context, id string) error {
	result, err := s.exec.ExecContext(ctx, `
		DELETE FROM blob_transfer_session WHERE session_id = ? AND session_kind = ?`,
		id, string(binary.TransferDownload),
	)
	if err != nil {
		return fmt.Errorf("delete download session: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete download session rows: %w", err)
	}
	if rows == 0 {
		return binary.ErrNotFound
	}
	return nil
}

func (s *BlobMetadataStore) scanManifest(ctx context.Context, query string, arg any) (*binary.BlobManifest, error) {
	var (
		m                   binary.BlobManifest
		contentType         sql.NullString
		chunkSize           sql.NullInt64
		createdAt           string
		finalizedAt         sql.NullString
		encryptionAlgorithm sql.NullString
		encryptionKeyID     sql.NullString
		encryptionNonce     sql.NullString
		retentionMode       sql.NullString
		retainUntil         sql.NullString
		backendKind         string
		storageRef          string
	)
	err := s.exec.QueryRowContext(ctx, query, arg).Scan(
		&m.Descriptor.BlobID, &m.Descriptor.SHA256, &m.Descriptor.Size,
		&contentType, &backendKind, &storageRef,
		&chunkSize, &m.ChunkCount, &createdAt, &finalizedAt,
		&encryptionAlgorithm, &encryptionKeyID, &encryptionNonce,
		&retentionMode, &retainUntil,
	)
	if err == sql.ErrNoRows {
		return nil, binary.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan blob manifest: %w", err)
	}
	if contentType.Valid {
		m.Descriptor.ContentType = contentType.String
	}
	m.Descriptor.Backend = binary.BackendKind(backendKind)
	m.Descriptor.Pointer = binary.StoragePointer{
		Backend: binary.BackendKind(backendKind),
		Ref:     storageRef,
	}
	if chunkSize.Valid {
		m.ChunkSize = chunkSize.Int64
	}
	if encryptionAlgorithm.Valid {
		m.Descriptor.Encryption = &binary.BlobEncryption{
			Algorithm: binary.EncryptionAlgorithm(encryptionAlgorithm.String),
			KeyID:     encryptionKeyID.String,
			Nonce:     encryptionNonce.String,
		}
	}
	if retentionMode.Valid && retainUntil.Valid {
		ts, err := parseTime(retainUntil.String)
		if err != nil {
			return nil, err
		}
		m.Descriptor.Retention = &binary.BlobRetention{
			Mode:        binary.RetentionMode(retentionMode.String),
			RetainUntil: ts,
		}
	}
	ca, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	m.CreatedAt = ca
	if finalizedAt.Valid {
		fa, err := parseTime(finalizedAt.String)
		if err != nil {
			return nil, err
		}
		m.FinalizedAt = &fa
	}
	return &m, nil
}

func nullInt(v int) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(v), Valid: true}
}

func nullInt64(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

func uploadEncryptionAlgorithm(session binary.UploadSession) string {
	if session.Encryption == nil {
		return ""
	}
	return string(session.Encryption.Algorithm)
}

func uploadEncryptionKeyID(session binary.UploadSession) string {
	if session.Encryption == nil {
		return ""
	}
	return session.Encryption.KeyID
}

func uploadEncryptionNonce(session binary.UploadSession) string {
	if session.Encryption == nil {
		return ""
	}
	return session.Encryption.Nonce
}

func uploadRetentionMode(session binary.UploadSession) string {
	if session.Retention == nil {
		return ""
	}
	return string(session.Retention.Mode)
}

func uploadRetainUntil(session binary.UploadSession) string {
	if session.Retention == nil || session.Retention.RetainUntil.IsZero() {
		return ""
	}
	return formatTime(session.Retention.RetainUntil)
}

func applyUploadPolicyFields(
	session *binary.UploadSession,
	encAlgorithm, encKeyID, encNonce, retentionMode, retainUntil sql.NullString,
) {
	if encAlgorithm.Valid {
		session.Encryption = &binary.BlobEncryption{
			Algorithm: binary.EncryptionAlgorithm(encAlgorithm.String),
			KeyID:     encKeyID.String,
			Nonce:     encNonce.String,
		}
	}
	if retentionMode.Valid && retainUntil.Valid {
		ts, err := parseTime(retainUntil.String)
		if err == nil {
			session.Retention = &binary.BlobRetention{
				Mode:        binary.RetentionMode(retentionMode.String),
				RetainUntil: ts,
			}
		}
	}
}
