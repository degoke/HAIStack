package sync

import (
	"strconv"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// EventType names a sync protocol event family.
type EventType string

const (
	EventTypeResourceCreated    EventType = "resource.created"
	EventTypeResourceUpdated    EventType = "resource.updated"
	EventTypeResourceDeleted    EventType = "resource.deleted"
	EventTypeResourceConflicted EventType = "resource.conflicted"
)

// LocalEventStatus tracks device-side sync state for one outbox-derived event.
type LocalEventStatus string

const (
	LocalEventStatusPending    LocalEventStatus = "pending"
	LocalEventStatusPushed     LocalEventStatus = "pushed"
	LocalEventStatusAccepted   LocalEventStatus = "accepted"
	LocalEventStatusRejected   LocalEventStatus = "rejected"
	LocalEventStatusConflicted LocalEventStatus = "conflicted"
	LocalEventStatusRetry      LocalEventStatus = "retry"
)

// LocalEvent is the rich sync protocol payload proposed from a device node.
type LocalEvent struct {
	EventID          string                  `json:"eventId"`
	OriginNodeID     string                  `json:"originNodeId"`
	TenantID         string                  `json:"tenantId"`
	ResourceType     string                  `json:"resourceType"`
	ResourceID       string                  `json:"resourceId"`
	Operation        EventType               `json:"operation"`
	BaseCloudVersion string                  `json:"baseCloudVersion,omitempty"`
	LocalVersion     string                  `json:"localVersion"`
	ChangedPaths     []string                `json:"changedPaths,omitempty"`
	Patch            []byte                  `json:"patch,omitempty"`
	ResourceAfter    *types.ResourceEnvelope `json:"resourceAfter,omitempty"`
	ResourceHash     string                  `json:"resourceHash,omitempty"`
	Status           LocalEventStatus        `json:"status"`
	CreatedAt        time.Time               `json:"createdAt"`
	OutboxSequence   int64                   `json:"outboxSequence,omitempty"`
}

// CanonicalStatus describes hub disposition for a canonical replay event.
type CanonicalStatus string

const (
	CanonicalStatusAccepted   CanonicalStatus = "accepted"
	CanonicalStatusRejected   CanonicalStatus = "rejected"
	CanonicalStatusConflicted CanonicalStatus = "conflicted"
)

// CanonicalEvent is a hub-assigned replay event returned to device nodes on pull.
type CanonicalEvent struct {
	EventID               string                  `json:"eventId"`
	OriginNodeID          string                  `json:"originNodeId,omitempty"`
	TenantID              string                  `json:"tenantId"`
	ResourceType          string                  `json:"resourceType"`
	ResourceID            string                  `json:"resourceId"`
	Operation             EventType               `json:"operation"`
	BaseCloudVersion      string                  `json:"baseCloudVersion,omitempty"`
	LocalVersion          string                  `json:"localVersion,omitempty"`
	ResourceAfter         *types.ResourceEnvelope `json:"resourceAfter,omitempty"`
	ResourceHash          string                  `json:"resourceHash,omitempty"`
	CanonicalSequence     int64                   `json:"canonicalSequence"`
	CanonicalVersionID    string                  `json:"canonicalVersionId"`
	Status                CanonicalStatus         `json:"status"`
	AcknowledgedAt        time.Time               `json:"acknowledgedAt,omitempty"`
	ConflictReason        string                  `json:"conflictReason,omitempty"`
	ConflictRemoteVersion string                  `json:"conflictRemoteVersion,omitempty"`
}

// CanonicalEventID returns the stable, tenant-scoped idempotency key for
// pull/apply dedupe.
func CanonicalEventID(tenantID string, sequence int64) string {
	return tenantID + ":canonical:" + formatSequence(sequence)
}

// OutboxEventID returns a stable client event id for one outbox row.
func OutboxEventID(nodeID, tenantID string, sequence int64) string {
	return tenantID + ":" + nodeID + ":outbox:" + formatSequence(sequence)
}

func formatSequence(sequence int64) string {
	return strconv.FormatInt(sequence, 10)
}
