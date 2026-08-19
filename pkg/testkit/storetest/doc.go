// Package storetest provides importable in-memory implementations of pkg/store
// interfaces used across Health AI Stack tests.
//
// # Implemented contracts
//
//   - ResourceStore (strict and lenient read semantics)
//   - HistoryStore, EventStore, CursorStore, InboxStore
//   - ConflictStore, SearchStore, AuditStore
//   - WriteSessionProvider (resource + history + search + events)
//
// Resource IDs are returned in stable lexical order. WriteSessionProvider
// snapshots all four stores and publishes them together on Commit; Rollback
// discards the session snapshot.
//
// Job persistence reuses jobs.NewInMemoryJobStore via Backend.Jobs rather than
// duplicating another mem job implementation.
//
// # Backend bundles
//
//   - NewDeviceBackend — lenient ResourceStore reads (nil, nil on missing) for sync device nodes
//   - NewStrictBackend — strict ResourceStore reads (error on missing) for core/store tests
//   - NewWriteSessionProvider — isolated write-session bundle for core integration tests
//
// # Strict vs lenient reads
//
// pkg/store/store_test.go and pkg/core tests expect strict semantics: Read returns
// an error when a resource is absent. pkg/sync device tests expect lenient semantics:
// Read returns nil, nil. Choose the constructor that matches the scenario.
//
// Compile-time interface checks are declared on each exported store type.
package storetest
