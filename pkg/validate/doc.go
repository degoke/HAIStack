// Package validate implements haistack-validate, the built-in FHIR resource validation
// library for health-ai-stack.
//
// haistack-validate checks canonical JSON resources before storage, sync, indexing, or AI
// exposure. It validates JSON shape, resource types, IDs, required fields, reference syntax,
// and structural/primitive constraints via the Google FHIR R4 proto layer.
// Optional profile cardinality checks run when a ProfileCatalog is configured
// and ValidateOptions enable base and/or declared profiles. The runtime enables
// base HL7 R4 StructureDefinition validation by default.
//
// # Two integration paths
//
// Standalone engine API:
//
//	eng, _ := validate.NewEngine(validate.Config{})
//	result, err := eng.Validate(ctx, envelope, validate.ValidateOptions{})
//	outcome := validate.ToOperationOutcome(result)
//
// pkg/core adapter (legacy hook shape):
//
//	svc, _ := core.NewResourceService(core.ResourceServiceConfig{
//	    Validator: validate.NewCoreValidator(eng, validate.ValidateOptions{}),
//	})
//
// # MVP scope
//
// Built-in validation is structural and safety-oriented, not full FHIR conformance. It covers:
//
//   - JSON validity and canonical resource object shape
//   - resourceType presence and known FHIR base type checks
//   - optional installed resource-type allowlists
//   - FHIR id syntax and optional RequireID enforcement
//   - minimal configured required-field checks
//   - syntactic Reference.reference validation
//   - proto/jsonformat structural and primitive validation
//   - optional StructureDefinition cardinality checks (meta.profile or explicit Profiles)
//
// Not a full HL7 validator. Still out of scope for the built-in engine:
//
//   - slicing, custom FHIRPath invariants, extension policy
//   - module-specific business rules
//
// Resource-type installation checks are allowlist-based and optional. When no
// ResourceTypeRegistry is configured, all known FHIR base resource types are allowed.
//
// Reference validation is syntactic only; references are not resolved for existence.
//
// When envelope.Proto is populated, matches the JSON resource type, and envelope.Hash
// still matches canonical JSON, structural validation reuses the attached proto instead
// of re-parsing through jsonformat. JSON remains canonical; hash mismatch or absent proto
// forces a fresh JSON parse.
//
// # Errors
//
// Engine.Validate returns structured issues in ValidationResult for user-data problems.
// The returned error is reserved for exceptional failures such as context cancellation
// or internal parser faults that prevent validation from completing.
//
// NewCoreValidator adapts invalid ValidationResult values into errors compatible with
// pkg/core's existing ErrorKindInvalid wrapping. ToOperationOutcome maps issues directly
// for CLI, import, sync, and AI guard flows without going through pkg/core.
//
// # File layout
//
//   - doc.go           — package documentation (this file)
//   - validate.go      — Validator interface for pkg/core
//   - api.go           — Engine, Config, ValidateOptions, ValidationResult types
//   - engine.go        — built-in validation engine
//   - proto_validate.go — proto reuse and JSON/proto consistency checks
//   - config.go        — Config validation for NewEngine
//   - core_adapter.go  — NewCoreValidator
//   - outcome.go       — ToOperationOutcome
//   - reference.go     — syntactic reference checks
//   - defaults.go      — built-in required-field defaults
//   - profile.go       — optional StructureDefinition cardinality checks
package validate
