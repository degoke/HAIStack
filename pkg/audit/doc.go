// Package audit implements haistack-audit, the shared audit event library for
// Health AI Stack.
//
// # Scope
//
// v1 builds on store.AuditStore as the persistence contract. This package owns
// the canonical audit event model, action names, append/query helpers, and
// adapters used by auth, AI, view, sync, exports, and future packages.
//
// It does not replace store.AuditStore. Postgres persistence remains in
// pkg/postgres (TenantDB.AuditStore). SQLite persistence is in pkg/sqlite
// (DB.AuditStore). Auth may emit decisions through this package but must not
// own audit storage.
//
// # Public API
//
//   - Event / Query — canonical event and filter shapes
//   - Logger / LoggerFunc — append seam
//   - StoreAdapter — bridges Event to store.AuditStore
//   - Emit helpers: LogResourceRead, LogResourceWrite, LogSyncEvent,
//     LogAIToolCall, LogAuthDecision, LogExport, LogBlobAccess, LogViewAccess
//   - Action and Outcome constants for consistent naming
//
// # Typical usage
//
//	logger := &audit.StoreAdapter{Store: auditStore}
//	_ = audit.LogAuthDecision(ctx, logger, audit.AuthDecisionEvent{
//	    Actor: "user-1", Tenant: "tenant-a", Allowed: true, Reason: "rule X",
//	})
//
// Package-specific seams (ai.AuditLogger, view.AuditLogger) adapt into these
// helpers so callers query one consistent shape regardless of producer.
package audit
