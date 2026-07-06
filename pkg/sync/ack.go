package sync

import "time"

// AckState is the per-event acknowledgement returned by the hub on push.
type AckState string

const (
	AckAccepted         AckState = "accepted"
	AckRejected         AckState = "rejected"
	AckConflicted       AckState = "conflicted"
	AckAlreadyProcessed AckState = "already_processed"
	AckNeedsRetry       AckState = "needs_retry"
)

// PushResult carries hub acknowledgement metadata for one proposed local event.
type PushResult struct {
	EventID                 string    `json:"eventId"`
	State                   AckState  `json:"state"`
	CanonicalSequence       int64     `json:"canonicalSequence,omitempty"`
	CanonicalVersionID      string    `json:"canonicalVersionId,omitempty"`
	RejectionReason         string    `json:"rejectionReason,omitempty"`
	ConflictReason          string    `json:"conflictReason,omitempty"`
	ConflictRemoteVersionID string    `json:"conflictRemoteVersionId,omitempty"`
	RetryAfter              time.Time `json:"retryAfter,omitempty"`
}

// IsTerminal reports whether the device can advance its push cursor past this event.
func (r PushResult) IsTerminal() bool {
	switch r.State {
	case AckAccepted, AckRejected, AckConflicted, AckAlreadyProcessed:
		return true
	default:
		return false
	}
}
