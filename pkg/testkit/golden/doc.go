// Package golden provides comparison helpers for types.OperationOutcome in tests.
//
// v1 supports inline and embedded golden JSON payloads only. File-backed golden
// workflows can be added later when a concrete need appears.
//
// CanonicalOutcomeJSON normalizes an OperationOutcome to stable JSON for comparison,
// including deterministic sorting of issue objects.
// AssertOutcomeEqual ignores formatting differences between two outcome values.
// AssertOutcomeMatchesGolden compares against an inline golden JSON string.
//
// DecodeOutcome, AssertOutcomeCode, and AssertOutcomeIssueCount are convenience
// wrappers for HTTP, client, core, and validate test assertions.
//
// Primary consumers: pkg/http, pkg/client, pkg/core, and pkg/validate tests that
// need stable OperationOutcome diagnostics without brittle string equality.
package golden
