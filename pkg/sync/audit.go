package sync

import "github.com/degoke/health-ai-stack/pkg/audit"

// Audit action identifiers written by the sync engine.
// Canonical definitions live in pkg/audit; re-exported here for callers.
const (
	AuditSyncAccepted        = audit.ActionSyncAccepted
	AuditSyncRejected        = audit.ActionSyncRejected
	AuditSyncConflicted      = audit.ActionSyncConflicted
	AuditDevicePushed        = audit.ActionDevicePushed
	AuditDevicePulled        = audit.ActionDevicePulled
	AuditConflictAutoMerged  = audit.ActionConflictAutoMerged
	AuditConflictNeedsReview = audit.ActionConflictNeedsReview
)
