package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// ResourceStore persists current resource state in SQLite.
type ResourceStore struct {
	exec queryExec
}

func newResourceStore(db *sql.DB) *ResourceStore {
	return &ResourceStore{exec: db}
}

func newResourceStoreTx(tx *sql.Tx) *ResourceStore {
	return &ResourceStore{exec: tx}
}

type queryExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func (s *ResourceStore) Create(ctx context.Context, res *types.ResourceEnvelope) error {
	if res == nil {
		return fmt.Errorf("resource envelope is nil")
	}
	exists, err := s.Exists(ctx, res.ResourceType, res.ID)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("resource already exists: %s/%s", res.ResourceType, res.ID)
	}

	_, err = s.exec.ExecContext(ctx, `
		INSERT INTO hai_resource (resource_type, id, version_id, last_updated, json, hash, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`,
		res.ResourceType, res.ID, res.VersionID, formatTime(res.LastUpdated), res.JSON, res.Hash,
	)
	if err != nil {
		return fmt.Errorf("create resource: %w", err)
	}
	return nil
}

func (s *ResourceStore) Read(ctx context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	var (
		versionID   string
		lastUpdated string
		jsonData    []byte
		hash        sql.NullString
	)
	err := s.exec.QueryRowContext(ctx, `
		SELECT version_id, last_updated, json, hash
		FROM hai_resource
		WHERE resource_type = ? AND id = ?`,
		resourceType, id,
	).Scan(&versionID, &lastUpdated, &jsonData, &hash)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("resource not found: %s/%s", resourceType, id)
	}
	if err != nil {
		return nil, fmt.Errorf("read resource: %w", err)
	}

	ts, err := parseTime(lastUpdated)
	if err != nil {
		return nil, err
	}

	env := &types.ResourceEnvelope{
		ResourceType: resourceType,
		ID:           id,
		VersionID:    versionID,
		LastUpdated:  ts,
		JSON:         jsonData,
	}
	if hash.Valid {
		env.Hash = hash.String
	}
	return env, nil
}

func (s *ResourceStore) Update(ctx context.Context, res *types.ResourceEnvelope) error {
	if res == nil {
		return fmt.Errorf("resource envelope is nil")
	}
	result, err := s.exec.ExecContext(ctx, `
		UPDATE hai_resource
		SET version_id = ?, last_updated = ?, json = ?, hash = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE resource_type = ? AND id = ?`,
		res.VersionID, formatTime(res.LastUpdated), res.JSON, res.Hash, res.ResourceType, res.ID,
	)
	if err != nil {
		return fmt.Errorf("update resource: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update resource rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("resource not found: %s/%s", res.ResourceType, res.ID)
	}
	return nil
}

// UpdateIfVersion atomically replaces a resource only when its current
// version matches expectedVersion. The wildcard version "*" matches any
// existing version.
func (s *ResourceStore) UpdateIfVersion(ctx context.Context, res *types.ResourceEnvelope, expectedVersion string) (bool, error) {
	if res == nil {
		return false, fmt.Errorf("resource envelope is nil")
	}
	query := `
		UPDATE hai_resource
		SET version_id = ?, last_updated = ?, json = ?, hash = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE resource_type = ? AND id = ?`
	args := []any{res.VersionID, formatTime(res.LastUpdated), res.JSON, res.Hash, res.ResourceType, res.ID}
	if expectedVersion != "*" {
		query += " AND version_id = ?"
		args = append(args, expectedVersion)
	}
	result, err := s.exec.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("conditional update resource: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("conditional update resource rows affected: %w", err)
	}
	return rows > 0, nil
}

func (s *ResourceStore) Delete(ctx context.Context, resourceType, id string) error {
	result, err := s.exec.ExecContext(ctx, `
		DELETE FROM hai_resource WHERE resource_type = ? AND id = ?`,
		resourceType, id,
	)
	if err != nil {
		return fmt.Errorf("delete resource: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete resource rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("resource not found: %s/%s", resourceType, id)
	}
	return nil
}

// DeleteIfVersion atomically deletes a resource only when its current version
// matches expectedVersion. The wildcard version "*" matches any existing
// version.
func (s *ResourceStore) DeleteIfVersion(ctx context.Context, resourceType, id, expectedVersion string) (bool, error) {
	query := `DELETE FROM hai_resource WHERE resource_type = ? AND id = ?`
	args := []any{resourceType, id}
	if expectedVersion != "*" {
		query += " AND version_id = ?"
		args = append(args, expectedVersion)
	}
	result, err := s.exec.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("conditional delete resource: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("conditional delete resource rows affected: %w", err)
	}
	return rows > 0, nil
}

func (s *ResourceStore) Exists(ctx context.Context, resourceType, id string) (bool, error) {
	var count int
	err := s.exec.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM hai_resource WHERE resource_type = ? AND id = ?`,
		resourceType, id,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("exists resource: %w", err)
	}
	return count > 0, nil
}

// ListIDs returns resource IDs for one type in stable id order.
func (s *ResourceStore) ListIDs(ctx context.Context, resourceType string, limit, offset int) ([]string, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.exec.QueryContext(ctx, `
		SELECT id FROM hai_resource
		WHERE resource_type = ?
		ORDER BY id
		LIMIT ? OFFSET ?`, resourceType, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list resource ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan resource id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate resource ids: %w", err)
	}
	return ids, nil
}
