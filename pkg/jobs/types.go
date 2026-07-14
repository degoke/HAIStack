package jobs

// Canonical job type prefixes and well-known types used across the stack.
// Package owners may define additional types; these constants document the
// shared naming conventions.
const (
	// TypePrefixSync scopes sync engine background jobs.
	TypePrefixSync = "sync."
	// TypePrefixSearch scopes search package background jobs.
	TypePrefixSearch = "search."
	// TypePrefixView scopes future view refresh jobs.
	TypePrefixView = "view."
	// TypePrefixAnalytics scopes future analytics refresh jobs.
	TypePrefixAnalytics = "analytics."
	// TypePrefixBinary scopes future blob cleanup jobs.
	TypePrefixBinary = "binary."
	// TypePrefixSubscriptions scopes future subscription delivery jobs.
	TypePrefixSubscriptions = "subscriptions."
	// TypePrefixExport scopes future export jobs.
	TypePrefixExport = "export."

	// TypeSearchReindex is the canonical dotted search reindex job type.
	TypeSearchReindex = TypePrefixSearch + "reindex"

	// TypeReindex is the search reindex job type (historical short name).
	TypeReindex = "reindex"

	// TypeSyncRetryPush schedules a device push retry.
	TypeSyncRetryPush = "sync.retry_push"
	// TypeSyncScheduledPull schedules a future pull attempt.
	TypeSyncScheduledPull = "sync.scheduled_pull"
	// TypeSyncConflictProcessing schedules conflict follow-up work.
	TypeSyncConflictProcessing = "sync.conflict_processing"
	// TypeSyncEventReplay schedules push/pull replay.
	TypeSyncEventReplay = "sync.event_replay"
)
