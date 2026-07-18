// Package terminology provides tenant-scoped terminology services for FHIR R4.
//
// CodeSystem and ValueSet resources remain canonical FHIR JSON resources. The
// package parses those resources into disposable projections used for fast
// lookup, validation, and finite ValueSet expansion. Projections can be
// replaced or rebuilt at any time from the canonical resource.
//
// LocalService is the initial local provider. Provider and Chain define the
// boundary for adding built-in, module, remote, or large terminology systems
// without changing callers. Terminology-aware validation is opt-in through
// pkg/validate; ordinary resource validation is unchanged by default.
package terminology
