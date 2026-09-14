package store

import (
	"context"
	"time"
)

// TerminologyInstallRecord tracks tenant opt-in to a global terminology pack.
type TerminologyInstallRecord struct {
	PackName     string    `json:"packName"`
	PackVersion  string    `json:"packVersion"`
	CanonicalURL string    `json:"canonicalUrl"`
	Version      string    `json:"version"`
	ResourceType string    `json:"resourceType"`
	Enabled      bool      `json:"enabled"`
	SourceModule string    `json:"sourceModule,omitempty"`
	InstalledAt  time.Time `json:"installedAt"`
}

// TerminologyInstallFilter selects terminology install rows.
type TerminologyInstallFilter struct {
	PackName     string
	CanonicalURL string
	Version      string
	ResourceType string
}

// TerminologyInstallStore persists per-tenant terminology pack enablement.
type TerminologyInstallStore interface {
	SetEnabled(ctx context.Context, record TerminologyInstallRecord) error
	UpsertInstall(ctx context.Context, record TerminologyInstallRecord) error
	ListEnabled(ctx context.Context) ([]TerminologyInstallRecord, error)
	ListInstalled(ctx context.Context, filter TerminologyInstallFilter) ([]TerminologyInstallRecord, error)
	Delete(ctx context.Context, filter TerminologyInstallFilter) error
}
