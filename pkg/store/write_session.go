package store

import "context"

// WriteSession coordinates transaction-scoped store access for one atomic write.
type WriteSession interface {
	ResourceStore() ResourceStore
	HistoryStore() HistoryStore
	SearchStore() SearchStore
	EventStore() EventStore
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// TerminologyWriteSession is optionally implemented by transaction sessions
// whose terminology projections share the resource write transaction.
type TerminologyWriteSession interface{ TerminologyStore() TerminologyStore }

// InboxWriteSession is implemented by database-backed write sessions that can
// include inbox idempotency records in the same transaction as resource,
// history, and search changes.
type InboxWriteSession interface {
	InboxStore() InboxStore
}

// WriteSessionProvider begins atomic write sessions.
type WriteSessionProvider interface {
	BeginWrite(ctx context.Context) (WriteSession, error)
}
