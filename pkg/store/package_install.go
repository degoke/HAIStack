package store

import (
	"context"
	"time"
)

// PackageInstallStore tracks completed FHIR package version installs.
type PackageInstallStore interface {
	MarkComplete(ctx context.Context, packageName, packageVersion string, completedAt time.Time) error
	IsComplete(ctx context.Context, packageName, packageVersion string) (bool, error)
}
