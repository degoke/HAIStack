// Package types implements haistack-types, a version-agnostic FHIR JSON type layer.
//
// haistack-types is the foundational library in the haistack monorepo. It lets every
// downstream package (pkg/core, pkg/store, pkg/proto, pkg/search, and others) handle FHIR
// resources generically without importing generated R4 or R5 structs. Resources are
// represented as normalized JSON envelopes with derived metadata, lightweight support types,
// and helpers for common field access.
//
// This package is the JSON source of truth for runtime abstractions:
//   - Canonical FHIR JSON is the primary interchange and storage form.
//   - ResourceEnvelope is the shared container passed across packages.
//   - Proto values (via pkg/proto) are optional typed companions — never a replacement for JSON.
//   - Hash, VersionID, and LastUpdated are always derived from canonical JSON, not from proto fields.
//
// MVP scope targets version-agnostic FHIR JSON. Generated R4/R5 bindings, bundle helpers,
// extension helpers, profile metadata, primitive validators, DB/REST/search/sync/auth, and AI
// tooling are explicitly out of scope here.
//
// # ResourceEnvelope
//
// ResourceEnvelope is the generic runtime container for any FHIR resource:
//
//   - ResourceType — top-level resourceType string.
//   - ID — top-level id when present (empty string when absent).
//   - VersionID — copied from meta.versionId when present.
//   - LastUpdated — parsed from meta.lastUpdated when present (zero time.Time when absent).
//   - JSON — normalized canonical JSON bytes (see Canonical JSON below).
//   - Hash — SHA-256 hex digest of JSON.
//   - Proto — optional typed companion; nil in JSON-only MVP paths (pkg/proto sets this).
//
// Envelope fields duplicate commonly accessed metadata so callers avoid re-parsing JSON for
// type, id, version, and timestamp on hot paths. JSON and Hash are always populated together
// from the same normalization pass.
//
// # ResourceCodec and JSONCodec
//
// ResourceCodec is the parsing boundary for FHIR JSON envelopes:
//
//   - ParseJSON(resourceType, data) decodes data, requires a top-level JSON object with
//     resourceType, normalizes the payload, derives envelope metadata, and returns
//     ResourceEnvelope. When resourceType is non-empty it must match the payload; when empty
//     the type is taken from the JSON.
//   - ToJSON(resource) returns resource.JSON when present; errors on nil resource or empty JSON.
//     There is no serialization path from Proto in MVP — callers must use pkg/proto for
//     proto-to-JSON conversion.
//
// JSONCodec is the default ResourceCodec implementation. Construct with NewJSONCodec.
// pkg/proto routes envelope construction through JSONCodec.ParseJSON so Hash and meta fields
// match JSON-only ingestion.
//
// # Canonical JSON
//
// NormalizeJSON defines the runtime's canonical JSON form:
//
//   - Input must be a valid JSON object (top-level array or scalar returns an error).
//   - Insignificant whitespace is removed.
//   - Object keys are sorted deterministically (encoding/json.Marshal on generic maps).
//   - Array element order is preserved.
//   - Nested objects and arrays are normalized recursively through the same rules.
//
// SetID, SetMeta, JSONCodec.ParseJSON, and envelope construction all return bytes in this
// canonical form. Semantically equivalent payloads that differ only in formatting or key order
// normalize to identical bytes.
//
// # Hashing
//
// HashResource returns the SHA-256 digest of normalized canonical JSON, hex-encoded.
// ResourceEnvelope.Hash is computed the same way during envelope construction. Equivalent
// payloads produce identical hashes; any semantic change produces a different hash. Hashing is
// content-only — it does not include resourceType as a separate input because resourceType is
// part of the JSON object.
//
// # JSON helpers
//
// Package-level helpers operate on raw []byte without requiring a ResourceEnvelope or typed
// struct. All mutating helpers (SetID, SetMeta) return normalized canonical bytes and preserve
// unrelated fields.
//
// GetResourceType returns the top-level resourceType or an error when missing/invalid.
//
// GetID returns the top-level id string, or "" when id is absent (not an error).
//
// SetID sets or removes the top-level id and returns normalized JSON.
//
// GetMeta extracts meta.versionId and meta.lastUpdated. Returns zero Meta when meta is absent.
// lastUpdated is parsed from FHIR ISO 8601 timestamps (RFC3339 and common FHIR variants).
//
// SetMeta updates meta.versionId and meta.lastUpdated. Empty VersionID removes versionId;
// zero LastUpdated removes lastUpdated. Removes the entire meta object when no fields remain.
//
// GetReferences recursively walks nested JSON objects and arrays and collects every field
// named reference (FHIR Reference.reference). Each value is parsed into Reference (see below).
// Discovery order follows JSON structure traversal (object keys in map iteration order during
// walk; not sorted).
//
// # Reference parsing
//
// GetReferences applies these rules when populating Reference fields:
//
//   - Typed relative references (ResourceType/ID) — e.g. Patient/123 — set ResourceType, ID,
//     and Raw. IDs after the first slash are retained in ID (e.g. Patient/123/_history/1).
//   - Untyped relative references (no slash, not a URL/URN/fragment) — e.g. only-an-id —
//     populate Raw only; ResourceType and ID remain empty.
//   - Absolute URLs (http://, https://), URNs (urn:), and fragments (#) — populate Raw only.
//
// Reference.Raw always holds the original reference string.
//
// # Support types
//
// Reference — parsed reference with optional ResourceType, ID, and always Raw.
//
// Meta — lightweight meta.versionId and meta.lastUpdated (not the full FHIR Meta complex type).
//
// Identifier — lightweight identifier fields (System, Value, Use, Type) for future helpers;
// no extraction helpers in MVP.
//
// OperationOutcome and OperationIssue — hand-written FHIR OperationOutcome types with JSON tags
// for direct marshaling (for example error responses from pkg/core). Code is a plain string in
// MVP, not a CodeableConcept.
//
// # Integration with other packages
//
// pkg/proto — ProtoToEnvelope converts proto → JSON → JSONCodec.ParseJSON, then sets
// envelope.Proto. Canonical JSON and Hash always come from pkg/types normalization.
//
// pkg/store — storage contracts use ResourceEnvelope and depend only on pkg/types for FHIR
// resource shape (no business rules in the type layer).
//
// pkg/core — resource lifecycle reads and writes envelopes; OperationOutcome types may be
// composed for API errors.
//
// # Typical flows
//
// JSON ingest:
//
//	FHIR JSON bytes → NewJSONCodec().ParseJSON("", data) → ResourceEnvelope { JSON, Hash, ... }
//
// JSON egress:
//
//	ResourceEnvelope → JSONCodec.ToJSON(envelope) → canonical JSON bytes
//
// Field mutation without envelope:
//
//	data → SetID(data, newID) → normalized JSON → HashResource(...) for updated digest
//
// Reference extraction:
//
//	data → GetReferences(data) → []Reference for indexing, graph walks, or validation
//
// Proto path (via pkg/proto):
//
//	proto → pkg/proto.ProtoToJSON → types.NormalizeJSON / JSONCodec.ParseJSON → envelope + Proto
//
// # File layout
//
//   - doc.go — package documentation (this file).
//   - types.go — Reference, Meta, Identifier.
//   - envelope.go — ResourceEnvelope.
//   - operation_outcome.go — OperationOutcome, OperationIssue.
//   - codec.go — ResourceCodec, JSONCodec.
//   - helpers.go — package-level JSON helpers.
//   - jsonutil.go — internal canonicalization, hashing, and reference parsing (unexported).
//
// # Out of scope (MVP)
//
// Bundle helpers, extension helpers, profile metadata, primitive validators, generated R4/R5
// adapters, identifier extraction helpers, DB/REST/search/sync/auth, AI tooling, and proto
// blob storage. Bundle ownership is acknowledged for a later phase; generic single-resource
// movement is the first-cut focus.
package types
