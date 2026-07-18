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

// WriteSessionProvider begins atomic write sessions.
type WriteSessionProvider interface {
	BeginWrite(ctx context.Context) (WriteSession, error)
}
