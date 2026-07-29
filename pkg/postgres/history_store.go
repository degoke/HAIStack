package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HistoryStore persists immutable resource version history in Postgres.
type HistoryStore struct {
	exec     querier
	tenantID string
}

func newHistoryStore(pool *pgxpool.Pool, tenantID string) *HistoryStore {
	return &HistoryStore{exec: pool, tenantID: tenantID}
}

func newHistoryStoreTx(tx pgx.Tx, tenantID string) *HistoryStore {
	return &HistoryStore{exec: tx, tenantID: tenantID}
}

func (s *HistoryStore) AppendVersion(ctx context.Context, version store.ResourceVersion) error {
	var jsonData []byte
	if version.Resource != nil {
		jsonData = version.Resource.JSON
	}

	_, err := s.exec.Exec(ctx, `
		INSERT INTO hai_resource_history (
			tenant_id, resource_type, resource_id, version_id, action, timestamp, hash, deleted, json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		s.tenantID,
		version.ResourceType,
		version.ID,
		version.VersionID,
		string(version.Action),
		version.Timestamp,
		nullString(version.Hash),
		version.Deleted,
		jsonData,
	)
	if err != nil {
		return fmt.Errorf("append history version: %w", err)
	}
	return nil
}

func (s *HistoryStore) GetHistory(ctx context.Context, resourceType, id string) ([]store.ResourceVersion, error) {
	rows, err := s.exec.Query(ctx, `
		SELECT version_id, action, timestamp, hash, deleted, json
		FROM hai_resource_history
		WHERE tenant_id = $1 AND resource_type = $2 AND resource_id = $3
		ORDER BY rowid ASC`,
		s.tenantID, resourceType, id,
	)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()

	var out []store.ResourceVersion
	for rows.Next() {
		var (
			versionID string
			action    string
			timestamp time.Time
			hash      *string
			deleted   bool
			jsonData  []byte
		)
		if err := rows.Scan(&versionID, &action, &timestamp, &hash, &deleted, &jsonData); err != nil {
			return nil, fmt.Errorf("scan history row: %w", err)
		}

		version := store.ResourceVersion{
			ResourceType: resourceType,
			ID:           id,
			VersionID:    versionID,
			Action:       store.VersionAction(action),
			Timestamp:    timestamp,
			Deleted:      deleted,
		}
		if hash != nil {
			version.Hash = *hash
		}
		if len(jsonData) > 0 {
			version.Resource = &types.ResourceEnvelope{
				ResourceType: resourceType,
				ID:           id,
				VersionID:    versionID,
				LastUpdated:  timestamp,
				JSON:         jsonData,
			}
			if hash != nil {
				version.Resource.Hash = *hash
			}
		}
		out = append(out, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate history: %w", err)
	}
	return out, nil
}
