package store

import (
	"context"
	"time"
)

// PackageInstallStore tracks completed FHIR package version installs.
// MarkComplete is idempotent: it records the first completion timestamp only.
type PackageInstallStore interface {
	MarkComplete(ctx context.Context, packageName, packageVersion string, completedAt time.Time) error
	IsComplete(ctx context.Context, packageName, packageVersion string) (bool, error)
}
