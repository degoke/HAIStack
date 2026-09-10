// Package infernotest provides Inferno SMART App Launch-aligned conformance
// checks against a reference HAIStack host backed by pkg/oauth.
//
// Phase 1 covers the Inferno smart_discovery_stu2 group: well-known endpoint
// retrieval and required SMART configuration fields. The reference host serves
// discovery at {fhir_base}/.well-known/smart-configuration.
//
// Use cmd/inferno-reference for manual Inferno test kit runs against a live host.
package infernotest
