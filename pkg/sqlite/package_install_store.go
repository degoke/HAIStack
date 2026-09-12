package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// PackageInstallStore tracks completed FHIR package version installs in SQLite.
type PackageInstallStore struct {
	exec queryExec
}

func newPackageInstallStore(e queryExec) *PackageInstallStore {
	return &PackageInstallStore{exec: e}
}

func (s *PackageInstallStore) MarkComplete(ctx context.Context, packageName, packageVersion string, completedAt time.Time) error {
	_, err := s.exec.ExecContext(ctx, `
		INSERT INTO hai_package_install (package_name, package_version, completed_at)
		VALUES (?, ?, ?)
		ON CONFLICT (package_name, package_version) DO NOTHING`,
		packageName, packageVersion, formatTime(completedAt),
	)
	if err != nil {
		return fmt.Errorf("mark package install complete: %w", err)
	}
	return nil
}

func (s *PackageInstallStore) IsComplete(ctx context.Context, packageName, packageVersion string) (bool, error) {
	row := s.exec.QueryRowContext(ctx, `
		SELECT 1
		FROM hai_package_install
		WHERE package_name = ? AND package_version = ?
		LIMIT 1`, packageName, packageVersion)
	var marker int
	if err := row.Scan(&marker); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("check package install complete: %w", err)
	}
	return true, nil
}
