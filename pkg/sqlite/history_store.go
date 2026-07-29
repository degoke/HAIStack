package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// HistoryStore persists immutable resource version history in SQLite.
type HistoryStore struct {
	exec historyExec
}

type historyExec interface {
	queryExec
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func newHistoryStore(db *sql.DB) *HistoryStore {
	return &HistoryStore{exec: db}
}

func newHistoryStoreTx(tx *sql.Tx) *HistoryStore {
	return &HistoryStore{exec: tx}
}

func (s *HistoryStore) AppendVersion(ctx context.Context, version store.ResourceVersion) error {
	var jsonData []byte
	if version.Resource != nil {
		jsonData = version.Resource.JSON
	}

	deleted := 0
	if version.Deleted {
		deleted = 1
	}

	_, err := s.exec.ExecContext(ctx, `
		INSERT INTO hai_resource_history (
			resource_type, resource_id, version_id, action, timestamp, hash, deleted, json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		version.ResourceType,
		version.ID,
		version.VersionID,
		string(version.Action),
		formatTime(version.Timestamp),
		version.Hash,
		deleted,
		jsonData,
	)
	if err != nil {
		return fmt.Errorf("append history version: %w", err)
	}
	return nil
}

func (s *HistoryStore) GetHistory(ctx context.Context, resourceType, id string) ([]store.ResourceVersion, error) {
	rows, err := s.exec.QueryContext(ctx, `
		SELECT version_id, action, timestamp, hash, deleted, json
		FROM hai_resource_history
		WHERE resource_type = ? AND resource_id = ?
		ORDER BY rowid ASC`,
		resourceType, id,
	)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []store.ResourceVersion
	for rows.Next() {
		var (
			versionID string
			action    string
			timestamp string
			hash      sql.NullString
			deleted   int
			jsonData  []byte
		)
		if err := rows.Scan(&versionID, &action, &timestamp, &hash, &deleted, &jsonData); err != nil {
			return nil, fmt.Errorf("scan history row: %w", err)
		}

		ts, err := parseTime(timestamp)
		if err != nil {
			return nil, err
		}

		version := store.ResourceVersion{
			ResourceType: resourceType,
			ID:           id,
			VersionID:    versionID,
			Action:       store.VersionAction(action),
			Timestamp:    ts,
			Deleted:      deleted != 0,
		}
		if hash.Valid {
			version.Hash = hash.String
		}
		if len(jsonData) > 0 {
			version.Resource = &types.ResourceEnvelope{
				ResourceType: resourceType,
				ID:           id,
				VersionID:    versionID,
				LastUpdated:  ts,
				JSON:         jsonData,
			}
			if hash.Valid {
				version.Resource.Hash = hash.String
			}
		}
		out = append(out, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate history: %w", err)
	}
	return out, nil
}
