// Package r4 exposes ergonomic aliases for the pinned Google FHIR R4 protobuf
// messages used by package proto.
//
// The aliases preserve the generated protobuf API while keeping applications
// independent of the provider module's long import paths. JSON remains the
// canonical representation; use proto.ToEnvelope to attach a typed R4 value to
// a types.ResourceEnvelope.
package r4

//go:generate ./generate.sh
