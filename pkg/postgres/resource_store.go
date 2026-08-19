package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ResourceStore persists current resource state in Postgres.
type ResourceStore struct {
	exec     querier
	tenantID string
}

func newResourceStore(pool *pgxpool.Pool, tenantID string) *ResourceStore {
	return &ResourceStore{exec: pool, tenantID: tenantID}
}

func newResourceStoreTx(tx pgx.Tx, tenantID string) *ResourceStore {
	return &ResourceStore{exec: tx, tenantID: tenantID}
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

	_, err = s.exec.Exec(ctx, `
		INSERT INTO hai_resource (tenant_id, resource_type, id, version_id, last_updated, json, hash, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())`,
		s.tenantID, res.ResourceType, res.ID, res.VersionID, res.LastUpdated, res.JSON, nullString(res.Hash),
	)
	if err != nil {
		return fmt.Errorf("create resource: %w", err)
	}
	return nil
}

func (s *ResourceStore) Read(ctx context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	var (
		versionID   string
		lastUpdated time.Time
		jsonData    []byte
		hash        *string
	)
	err := s.exec.QueryRow(ctx, `
		SELECT version_id, last_updated, json, hash
		FROM hai_resource
		WHERE tenant_id = $1 AND resource_type = $2 AND id = $3`,
		s.tenantID, resourceType, id,
	).Scan(&versionID, &lastUpdated, &jsonData, &hash)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("resource not found: %s/%s", resourceType, id)
	}
	if err != nil {
		return nil, fmt.Errorf("read resource: %w", err)
	}

	env := &types.ResourceEnvelope{
		ResourceType: resourceType,
		ID:           id,
		VersionID:    versionID,
		LastUpdated:  lastUpdated,
		JSON:         jsonData,
	}
	if hash != nil {
		env.Hash = *hash
	}
	return env, nil
}

func (s *ResourceStore) Update(ctx context.Context, res *types.ResourceEnvelope) error {
	if res == nil {
		return fmt.Errorf("resource envelope is nil")
	}
	tag, err := s.exec.Exec(ctx, `
		UPDATE hai_resource
		SET version_id = $1, last_updated = $2, json = $3, hash = $4, updated_at = now()
		WHERE tenant_id = $5 AND resource_type = $6 AND id = $7`,
		res.VersionID, res.LastUpdated, res.JSON, nullString(res.Hash),
		s.tenantID, res.ResourceType, res.ID,
	)
	if err != nil {
		return fmt.Errorf("update resource: %w", err)
	}
	if tag.RowsAffected() == 0 {
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
		SET version_id = $1, last_updated = $2, json = $3, hash = $4, updated_at = now()
		WHERE tenant_id = $5 AND resource_type = $6 AND id = $7`
	args := []any{res.VersionID, res.LastUpdated, res.JSON, nullString(res.Hash), s.tenantID, res.ResourceType, res.ID}
	if expectedVersion != "*" {
		query += " AND version_id = $8"
		args = append(args, expectedVersion)
	}
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("conditional update resource: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (s *ResourceStore) Delete(ctx context.Context, resourceType, id string) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM hai_resource
		WHERE tenant_id = $1 AND resource_type = $2 AND id = $3`,
		s.tenantID, resourceType, id,
	)
	if err != nil {
		return fmt.Errorf("delete resource: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("resource not found: %s/%s", resourceType, id)
	}
	return nil
}

// DeleteIfVersion atomically deletes a resource only when its current version
// matches expectedVersion. The wildcard version "*" matches any existing
// version.
func (s *ResourceStore) DeleteIfVersion(ctx context.Context, resourceType, id, expectedVersion string) (bool, error) {
	query := `DELETE FROM hai_resource WHERE tenant_id = $1 AND resource_type = $2 AND id = $3`
	args := []any{s.tenantID, resourceType, id}
	if expectedVersion != "*" {
		query += " AND version_id = $4"
		args = append(args, expectedVersion)
	}
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("conditional delete resource: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (s *ResourceStore) Exists(ctx context.Context, resourceType, id string) (bool, error) {
	var count int
	err := s.exec.QueryRow(ctx, `
		SELECT COUNT(1) FROM hai_resource
		WHERE tenant_id = $1 AND resource_type = $2 AND id = $3`,
		s.tenantID, resourceType, id,
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
	rows, err := s.exec.Query(ctx, `
		SELECT id FROM hai_resource
		WHERE tenant_id = $1 AND resource_type = $2
		ORDER BY id
		LIMIT $3 OFFSET $4`, s.tenantID, resourceType, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list resource ids: %w", err)
	}
	defer rows.Close()

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
