package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IDRegistry manages authoritative resource ID registration per tenant.
type IDRegistry struct {
	exec     execer
	tenantID string
}

func newIDRegistry(pool *pgxpool.Pool, tenantID string) *IDRegistry {
	return &IDRegistry{exec: pool, tenantID: tenantID}
}

func newIDRegistryTx(tx pgx.Tx, tenantID string) *IDRegistry {
	return &IDRegistry{exec: tx, tenantID: tenantID}
}

func (s *IDRegistry) Check(ctx context.Context, resourceType, id string) (bool, error) {
	var count int
	err := s.exec.QueryRow(ctx, `
		SELECT COUNT(1) FROM hai_resource_id_registry
		WHERE tenant_id = $1 AND resource_type = $2 AND id = $3`,
		s.tenantID, resourceType, id,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check id registry: %w", err)
	}
	return count > 0, nil
}

func (s *IDRegistry) Reserve(ctx context.Context, resourceType, id string) error {
	tag, err := s.exec.Exec(ctx, `
		INSERT INTO hai_resource_id_registry (tenant_id, resource_type, id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`,
		s.tenantID, resourceType, id,
	)
	if err != nil {
		return fmt.Errorf("reserve id: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("id already registered: %s/%s", resourceType, id)
	}
	return nil
}

func (s *IDRegistry) Register(ctx context.Context, resourceType, id string) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO hai_resource_id_registry (tenant_id, resource_type, id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`,
		s.tenantID, resourceType, id,
	)
	if err != nil {
		return fmt.Errorf("register id: %w", err)
	}
	return nil
}
