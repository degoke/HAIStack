package sync

import "github.com/degoke/health-ai-stack/pkg/jobs"

// Job type identifiers enqueued by the sync engine.
// Canonical names live in pkg/jobs; re-exported here for callers.
const (
	JobTypeRetryPush          = jobs.TypeSyncRetryPush
	JobTypeScheduledPull      = jobs.TypeSyncScheduledPull
	JobTypeConflictProcessing = jobs.TypeSyncConflictProcessing
	JobTypeEventReplay        = jobs.TypeSyncEventReplay
)

// ReplayJobPayload schedules a push or pull replay attempt.
type ReplayJobPayload struct {
	NodeID       string `json:"nodeId"`
	TenantID     string `json:"tenantId"`
	FromSequence int64  `json:"fromSequence,omitempty"`
	EventID      string `json:"eventId,omitempty"`
	CanonicalSeq int64  `json:"canonicalSequence,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// ConflictJobPayload schedules conflict follow-up processing.
type ConflictJobPayload struct {
	NodeID          string `json:"nodeId"`
	TenantID        string `json:"tenantId"`
	ConflictID      string `json:"conflictId,omitempty"`
	EventID         string `json:"eventId"`
	ResourceType    string `json:"resourceType"`
	ResourceID      string `json:"resourceId"`
	LocalVersionID  string `json:"localVersionId"`
	RemoteVersionID string `json:"remoteVersionId"`
	Reason          string `json:"reason"`
	// LocalEvent carries the full sync payload so the conflict engine can
	// evaluate the local intent without needing to reconstruct it from history.
	LocalEvent []byte `json:"localEvent,omitempty"`
}

// RetryPushJobPayload is a narrow alias for push retry scheduling.
type RetryPushJobPayload struct {
	NodeID   string `json:"nodeId"`
	TenantID string `json:"tenantId"`
	EventID  string `json:"eventId"`
	Reason   string `json:"reason,omitempty"`
}
