// Package fhirpathtest provides convenience wrappers around pkg/fhirpath for tests.
//
// No custom FHIRPath engine logic lives here — only assertion helpers that evaluate
// expressions against *types.ResourceEnvelope fixtures (or envelope.Proto when set).
//
//   - DefaultEngine — fhirpath.NewEngine with default config
//   - Eval — evaluate and return stringified result values
//   - AssertValues, AssertString, AssertEmpty, AssertContains — test assertions
//
// Use fixtures or factories to supply envelopes; use this package to assert
// expression results without duplicating engine setup in every test file.
package fhirpathtest
