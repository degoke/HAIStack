package sync

// Audit action identifiers written by the sync engine.
const (
	AuditSyncAccepted        = "sync.accepted"
	AuditSyncRejected        = "sync.rejected"
	AuditSyncConflicted      = "sync.conflicted"
	AuditDevicePushed        = "sync.device_pushed"
	AuditDevicePulled        = "sync.device_pulled"
	AuditConflictAutoMerged  = "conflict.auto_merged"
	AuditConflictNeedsReview = "conflict.needs_review"
)
