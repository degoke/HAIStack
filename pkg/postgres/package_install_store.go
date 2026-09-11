package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PackageInstallStore tracks completed FHIR package version installs in Postgres.
type PackageInstallStore struct {
	exec querier
}

func newPackageInstallStore(pool *pgxpool.Pool) *PackageInstallStore {
	return &PackageInstallStore{exec: pool}
}

func (s *PackageInstallStore) MarkComplete(ctx context.Context, packageName, packageVersion string, completedAt time.Time) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO hai_package_install (package_name, package_version, completed_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (package_name, package_version) DO UPDATE SET
			completed_at = EXCLUDED.completed_at`,
		packageName, packageVersion, completedAt,
	)
	if err != nil {
		return fmt.Errorf("mark package install complete: %w", err)
	}
	return nil
}

func (s *PackageInstallStore) IsComplete(ctx context.Context, packageName, packageVersion string) (bool, error) {
	var marker int
	err := s.exec.QueryRow(ctx, `
		SELECT 1
		FROM hai_package_install
		WHERE package_name = $1 AND package_version = $2
		LIMIT 1`, packageName, packageVersion).Scan(&marker)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check package install complete: %w", err)
	}
	return true, nil
}
