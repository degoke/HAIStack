// Package proto implements haistack-proto, an adapter layer between provider-specific
// FHIR protobuf messages and the haistack runtime.
//
// haistack-proto wraps generated FHIR protos (initially Google FHIR Go R4) behind stable
// interfaces so downstream packages can parse, transform, and validate resources without
// importing provider module paths or version-specific message types. Callers work with
// any for proto values and *types.ResourceEnvelope for runtime containers.
//
// This package is an adapter, not a source of truth:
//   - Canonical FHIR JSON remains the primary stored form (resource_json).
//   - types.ResourceEnvelope is the shared runtime abstraction across packages.
//   - Proto values are optional typed companions for validation, transformation, and
//     (eventually) binary storage — never a replacement for JSON.
//
// # ProtoCodec
//
// ProtoCodec is the provider-agnostic boundary. Implementations convert between FHIR JSON,
// provider protobuf messages, and types.ResourceEnvelope:
//
//   - ParseJSONToProto validates the payload resourceType, parses JSON into a proto message,
//     and returns it as any.
//   - ProtoToJSON accepts a supported proto value, serializes to FHIR JSON, and re-normalizes
//     through types.NormalizeJSON so output matches the runtime's canonical JSON rules.
//   - ProtoToEnvelope converts proto to JSON, parses via types.NewJSONCodec().ParseJSON,
//     attaches the original proto to envelope.Proto, and returns a fully populated envelope
//     with Hash, VersionID, and LastUpdated derived the same way as JSON-only resources.
//
// When resourceType is non-empty on any method, it must match the actual resource type
// carried by the payload or proto value; mismatches return an error.
//
// # GoogleR4Codec
//
// GoogleR4Codec is the default ProtoCodec implementation. It uses github.com/google/fhir/go
// (pinned to v0.7.4) jsonformat support and R4 generated protos.
//
// ParseJSONToProto uses UnmarshalR4 and returns a Google R4 ContainedResource as any.
// ContainedResource is preferred internally so arbitrary resource types share one parse path.
//
// ProtoToJSON accepts either a ContainedResource or an individual R4 resource message
// (for example *patient_go_proto.Patient). Individual messages are marshaled via
// MarshalResource; ContainedResource values use Marshal.
//
// NewGoogleR4Codec constructs a codec with a UTC timezone unmarshaller and a compact
// (non-indented) marshaller. Initialization panics only on programmer error (invalid
// jsonformat configuration).
//
// # Integration with types.ResourceEnvelope
//
// ProtoToEnvelope always routes metadata through types.JSONCodec rather than reading proto
// fields directly. That guarantees envelope.Hash, envelope.VersionID, and envelope.LastUpdated
// match what pkg/types would produce from the same canonical JSON. envelope.Proto holds the
// caller's original proto value (ContainedResource or individual resource) for later typed
// access within pkg/proto or provider-aware code paths.
//
// Downstream storage and APIs should continue to persist and exchange envelope.JSON; Proto
// is an in-memory companion only in MVP (no resource_proto_blob APIs).
//
// # Resource registry
//
// google_r4_registry.go builds an internal r4ResourceRegistry from the R4 ContainedResource
// protobuf oneof at init time. The registry maps FHIR resource type names (for example Patient)
// to oneof field descriptors and message full names. It powers ResourceTypeOfProto,
// IsProtoResource, wrapR4Resource, and future provider-specific lookups without exposing
// Google types in the public API.
//
// # Convenience helpers
//
// IsProtoResource reports whether a value is a supported Google FHIR R4 protobuf resource
// (ContainedResource with a set branch, or a known individual R4 resource message).
//
// ResourceTypeOfProto returns the FHIR resource type string for a supported R4 proto value.
//
// Both helpers currently target R4 only. When additional providers or FHIR versions are
// added, they will dispatch by message descriptor (see google_r5.go).
//
// ToEnvelope, ToJSON, and ResourceType are concise R4 helpers using the default
// GoogleR4Codec. Applications can construct resources with aliases from
// github.com/degoke/health-ai-stack/pkg/proto/r4, for example:
//
//	patient := &r4.Patient{}
//	envelope, err := proto.ToEnvelope(patient)
//
// # Public API rules
//
// Exported codec signatures use any and *types.ResourceEnvelope. The r4 subpackage exposes
// aliases for the pinned Google-generated R4 messages; callers need not import the long
// github.com/google/fhir/go paths directly.
//
// Storage APIs for proto blobs, profile-aware validation, and proto diffing are out of
// MVP scope.
//
// # Conversion flows
//
// JSON ingest:
//
//	FHIR JSON → ParseJSONToProto → ContainedResource (any)
//	FHIR JSON → ParseJSONToProto → ProtoToEnvelope → ResourceEnvelope { JSON, Hash, Proto, ... }
//
// JSON egress:
//
//	ContainedResource (any) → ProtoToJSON → canonical JSON bytes
//	individual R4 resource (any) → ProtoToJSON → canonical JSON bytes
//
// Envelope construction:
//
//	proto (any) → ProtoToJSON → types.JSONCodec.ParseJSON → envelope + envelope.Proto = proto
//
// # Future extension points
//
// The following seams are documented in stub files and are not implemented in MVP:
//
//   - google_r5.go — GoogleR5Codec implementing ProtoCodec for R5 ContainedResource
//   - binary.go — wire-format marshal/unmarshal for optional proto blob storage
//   - validation.go — typed structural validation beyond jsonformat ingest checks
//   - codegen.go — generated registry and adapter boilerplate for new providers/versions
//
// Adding a new provider should implement ProtoCodec, keep provider imports internal, reuse
// types.JSONCodec for envelope normalization, and extend the contract tests in proto_test.go.
//
// # Out of scope (MVP)
//
// R5 support, binary proto storage helpers, typed profile validation, code generation
// scripts, proto diffing, and persistence of proto blobs alongside resource_json.
package proto
