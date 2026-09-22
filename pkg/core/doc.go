// Package core implements haistack-core, the FHIR runtime kernel for haistack.
//
// haistack-core owns resource lifecycle rules for CRUD, per-resource history,
// OperationOutcome mapping, and transaction-bundle execution while remaining
// storage- and transport-agnostic. It sits between HTTP/gRPC handlers (future) and
// pkg/store adapters (pkg/sqlite, pkg/postgres) without importing either backend.
//
// # Design principles
//
// Storage-agnostic orchestration:
//
//   - ResourceService depends on store.ResourceStore, store.HistoryStore, and
//     store.WriteSessionProvider — never on concrete database packages.
//   - Reads use connection-scoped stores; writes use WriteSession for atomicity.
//   - Version assignment, meta mutation, and hash recomputation happen in core,
//     not in store adapters (except postgres.ApplyWrite convenience path, which
//     core does not use).
//
// Canonical JSON first:
//
//   - Incoming payloads are normalized through pkg/types before lifecycle logic.
//   - Create, update, and delete recompute meta.versionId, meta.lastUpdated, and
//     ResourceEnvelope.Hash after metadata mutation.
//   - types.ResourceEnvelope.JSON remains the source of truth for content.
//
// Optional collaborators:
//
//   - validate.Validator — structural or profile checks before ID/version mutation.
//   - search.Indexer — produces store.SearchIndexEntry values on successful writes.
//   - sync.Outbox — when configured, appends store.ResourceEvent on successful writes.
//   - Omitted collaborators are no-ops; writes proceed without validation, indexing,
//     or event emission.
//
// Typed errors, not dual returns:
//
//   - Service methods return (*types.ResourceEnvelope, error) or error.
//   - Callers map errors to FHIR OperationOutcome via OperationOutcomeFromError.
//   - ErrorKind values align with FHIR issue codes: invalid, conflict, not-found,
//     not-supported, and exception.
//
// # ResourceService
//
// ResourceService is the primary entry point. Construct with NewResourceService and
// ResourceServiceConfig:
//
// Required dependencies:
//
//   - Resources — store.ResourceStore for Read and Exists checks outside sessions.
//   - History   — store.HistoryStore for per-resource History reads.
//   - Sessions  — store.WriteSessionProvider (for example sqlite.DB or postgres.TenantDB).
//   - IDPolicy  — ResourceIDPolicy; defaults to DefaultIDPolicy when nil.
//   - Codec     — types.ResourceCodec; defaults to types.NewJSONCodec() when nil.
//
// Optional dependencies:
//
//   - Validator — validate.Validator invoked before write-side ID/version mutation.
//   - Indexer   — search.Indexer invoked after resource/history persistence in session.
//   - Outbox    — sync.Outbox; when non-nil, events are appended via
//     sync.WithWriteSession during each write (transactional through session EventStore).
//   - Hooks     — optional four-point intercept SPI; core runs pre-storage before persist
//     and post-commit after a successful session commit.
//   - EnforceReferentialIntegrity — HAPI-style enforceReferentialIntegrityOnWrite;
//     nil or true (NewResourceService default) runs the write-time Exists check;
//     false skips checkReferentialIntegrity.
//
// Methods:
//
//   - Create(ctx, resource) — requires resourceType; accepts caller id or generates one;
//     rejects duplicate current resources as conflict.
//   - Read(ctx, resourceType, id) — returns current state or not-found.
//   - Update(ctx, resource) — requires resourceType and id; requires existing resource.
//   - Delete(ctx, resourceType, id) — reads current resource first so tombstone history
//     and outbox events carry the content hash; assigns a new tombstone versionId.
//   - History(ctx, resourceType, id) — returns ordered immutable ResourceVersion entries.
//   - ProcessTransactionBundle(ctx, bundle) — executes type=transaction bundles atomically.
//
// # Write pipeline
//
// Every successful write inside a WriteSession:
//
//  1. Normalize and parse incoming JSON (types + codec).
//  2. Validate with optional Validator.
//  3. Resolve or generate id via ResourceIDPolicy.
//  4. Optional pre-storage (if configured; no-op when hooks are nil).
//  5. Enforce referential integrity for local typed relative references
//     (types.GetReferences + ResourceStore.Exists) unless
//     EnforceReferentialIntegrity is false. Contained fragments, absolute
//     URLs, untyped ids, unresolved URNs, and self-references are skipped.
//     Intra-bundle POST/PUT identities satisfy Exists so transaction entry
//     order does not matter. Matching urn:uuid fullUrl values are rewritten
//     to the target entry Type/id before this check and persist.
//  6. Generate a new versionId (UUID) and set meta.versionId / meta.lastUpdated.
//  7. Recompute normalized JSON and hash.
//  8. Persist current resource state (create, update, or delete).
//  9. Append immutable history entry (ResourceVersion).
//
// 10. Append outbox event when Outbox is configured (via sync.WithWriteSession).
//
// 11. Rebuild search index entries when Indexer is configured.
//
// 12. Commit session; rollback on any failure before commit.
//
// Persist preparation (prepareWrite) is always pre-storage, then referential
// integrity, then version meta, so a pre-storage rewrite can satisfy Exists
// and integrity cannot run first.
//
// Delete path reads the current envelope before removal so history tombstones and events
// reference the last known content hash while receiving a new tombstone versionId.
//
// # Transaction bundles
//
// ProcessTransactionBundle supports only:
//
//   - Bundle.resourceType = "Bundle"
//   - Bundle.type = "transaction"
//   - Entry methods: POST, PUT, DELETE
//   - POST url format: ResourceType
//   - PUT and DELETE url format: ResourceType/id
//
// Rejected with not-supported:
//
//   - batch and other bundle types
//   - PATCH, GET, and other HTTP methods
//   - conditional URLs (query strings in request url)
//
// Entries execute in order inside one WriteSession. Referential integrity
// treats POST/PUT identities from every entry as already present, so an
// Observation may precede the Patient it references in the same bundle.
// Matching urn:uuid fullUrl values are rewritten to the target entry's
// Type/id in stored resource JSON (Patient/{assigned-id}) before persist.
// The response is a transaction-response Bundle envelope with per-entry
// status, location, etag, and lastModified.
//
// # Resource ID policy
//
// ResourceIDPolicy defines Validate(resourceType, id) and Generate(resourceType).
// DefaultIDPolicy accepts ids matching FHIR id syntax ([A-Za-z0-9\-\.]{1,64}) and
// generates UUIDs when the caller omits an id on create.
//
// # Errors and OperationOutcome
//
// ServiceError carries Kind, Message, optional Expression paths, and an optional Cause.
// KindOf, IsNotFound, and IsConflict classify errors for handlers.
//
// OperationOutcomeFromError maps ServiceError (including wrapped errors) to
// types.OperationOutcome with a single OperationIssue. Unrecognized errors map to
// exception severity/code.
//
// # Integration with other packages
//
// pkg/types   — ResourceEnvelope, JSONCodec, NormalizeJSON, SetMeta, HashResource.
// pkg/store   — storage contracts and WriteSession for atomic writes.
// pkg/validate — optional Validator before writes.
// pkg/search  — optional Indexer producing SearchIndexEntry values.
// pkg/sync    — optional Outbox and WithWriteSession for transactional event append.
// pkg/sqlite  — embedded local WriteSessionProvider (wired by application layer).
// pkg/postgres — tenant-scoped WriteSessionProvider (wired by application layer).
//
// # Typical flows
//
// Single resource create:
//
//	svc, _ := core.NewResourceService(core.ResourceServiceConfig{
//	    Resources: db.ResourceStore(),
//	    History:   db.HistoryStore(),
//	    Sessions:  db,
//	    Outbox:    &sync.EventStoreOutbox{Events: db.OutboxStore()},
//	})
//	created, err := svc.Create(ctx, &types.ResourceEnvelope{
//	    ResourceType: "Patient",
//	    JSON:         patientJSON,
//	})
//
// Error to OperationOutcome:
//
//	if err != nil {
//	    outcome := core.OperationOutcomeFromError(err)
//	    // marshal outcome as FHIR JSON response
//	}
//
// Transaction bundle:
//
//	resp, err := svc.ProcessTransactionBundle(ctx, bundleEnvelope)
//
// # File layout
//
//   - doc.go              — package documentation (this file)
//   - service.go          — ResourceService, write pipeline, CRUD, History
//   - bundle.go           — ProcessTransactionBundle and entry execution
//   - errors.go           — ServiceError, ErrorKind, classification helpers
//   - operation_outcome.go — OperationOutcomeFromError
//   - id_policy.go        — ResourceIDPolicy, DefaultIDPolicy
//
// # Testing
//
// core_test.go contains unit tests with in-memory store doubles covering CRUD, history,
// conflict rollback, optional collaborator failures, bundle execution, and
// OperationOutcome mapping.
//
// integration_sqlite_test.go and integration_postgres_test.go exercise ResourceService
// against real sqlite and postgres backends (postgres skips when Docker/DSN unavailable).
//
// service_internal_test.go contains package-internal tests for outbox wiring.
//
// # Out of scope (MVP)
//
// CapabilityStatement generation, conditional create/update, FHIR Patch, batch bundles,
// custom operations, system-wide _history, search/query execution, authentication,
// authorization, subscription delivery, and HTTP routing. Per-resource history only;
// no _history endpoint semantics in this increment.
package core
