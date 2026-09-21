package store

import (
	"context"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// VersionAction describes the lifecycle action recorded in resource history.
type VersionAction string

const (
	VersionActionCreate VersionAction = "create"
	VersionActionUpdate VersionAction = "update"
	VersionActionDelete VersionAction = "delete"
)

// ResourceVersion is an immutable history entry for one resource version.
// Delete history is represented as Action=VersionActionDelete with Deleted=true.
// Callers must supply VersionID; this package does not assign versions.
type ResourceVersion struct {
	ResourceType string                  `json:"resourceType"`
	ID           string                  `json:"id"`
	VersionID    string                  `json:"versionId"`
	Action       VersionAction           `json:"action"`
	Timestamp    time.Time               `json:"timestamp"`
	Resource     *types.ResourceEnvelope `json:"resource,omitempty"`
	Hash         string                  `json:"hash,omitempty"`
	Deleted      bool                    `json:"deleted"`
}

// HistoryStore appends and reads immutable resource version history.
type HistoryStore interface {
	AppendVersion(ctx context.Context, version ResourceVersion) error
	GetHistory(ctx context.Context, resourceType, id string) ([]ResourceVersion, error)
	GetVersion(ctx context.Context, resourceType, id, versionID string) (ResourceVersion, error)
}
