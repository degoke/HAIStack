package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NodeRegistry persists edge and cloud node metadata in Postgres.
type NodeRegistry struct {
	exec     querier
	tenantID string
}

func newNodeRegistry(pool *pgxpool.Pool, tenantID string) *NodeRegistry {
	return &NodeRegistry{exec: pool, tenantID: tenantID}
}

func (s *NodeRegistry) Register(ctx context.Context, node store.NodeRecord) error {
	metadata, err := json.Marshal(node.Metadata)
	if err != nil {
		return fmt.Errorf("marshal node metadata: %w", err)
	}
	_, err = s.exec.Exec(ctx, `
		INSERT INTO hai_node_registry (tenant_id, node_id, metadata, registered_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, node_id) DO UPDATE SET
			metadata = EXCLUDED.metadata,
			registered_at = EXCLUDED.registered_at`,
		s.tenantID, node.NodeID, metadata, node.RegisteredAt,
	)
	if err != nil {
		return fmt.Errorf("register node: %w", err)
	}
	return nil
}

func (s *NodeRegistry) Get(ctx context.Context, nodeID string) (*store.NodeRecord, error) {
	var (
		record     store.NodeRecord
		metadata   []byte
		registered time.Time
	)
	err := s.exec.QueryRow(ctx, `
		SELECT node_id, metadata, registered_at
		FROM hai_node_registry
		WHERE tenant_id = $1 AND node_id = $2`, s.tenantID, nodeID,
	).Scan(&record.NodeID, &metadata, &registered)
	if isNoRows(err) {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}
	if err != nil {
		return nil, fmt.Errorf("get node: %w", err)
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &record.Metadata)
	}
	record.RegisteredAt = registered
	return &record, nil
}

func (s *NodeRegistry) List(ctx context.Context) ([]store.NodeRecord, error) {
	rows, err := s.exec.Query(ctx, `
		SELECT node_id, metadata, registered_at
		FROM hai_node_registry
		WHERE tenant_id = $1
		ORDER BY node_id ASC`, s.tenantID)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()

	var out []store.NodeRecord
	for rows.Next() {
		var (
			record     store.NodeRecord
			metadata   []byte
			registered time.Time
		)
		if err := rows.Scan(&record.NodeID, &metadata, &registered); err != nil {
			return nil, fmt.Errorf("scan node row: %w", err)
		}
		if len(metadata) > 0 {
			_ = json.Unmarshal(metadata, &record.Metadata)
		}
		record.RegisteredAt = registered
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes: %w", err)
	}
	return out, nil
}

func (s *NodeRegistry) Unregister(ctx context.Context, nodeID string) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM hai_node_registry WHERE tenant_id = $1 AND node_id = $2`, s.tenantID, nodeID)
	if err != nil {
		return fmt.Errorf("unregister node: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	return nil
}
