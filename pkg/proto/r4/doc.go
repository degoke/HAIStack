// Package r4 exposes ergonomic aliases for the pinned Google FHIR R4 protobuf
// messages used by package proto.
//
// The aliases preserve the generated protobuf API while keeping applications
// independent of the provider module's long import paths. JSON remains the
// canonical representation; use proto.ToEnvelope to attach a typed R4 value to
// a types.ResourceEnvelope.
//
// Constructor helpers such as NewPatient and NewObservation set common fields
// without requiring direct imports of Google's generated sub-packages.
package r4

//go:generate ./generate.sh
