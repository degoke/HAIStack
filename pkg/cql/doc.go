// Package cql implements a bounded Clinical Quality Language (CQL) engine for
// HAIStack SDC questionnaires and in-memory clinical reasoning.
//
// The engine evaluates CQL libraries and inline expressions against a Patient
// context. It is not a full CQL 1.5 implementation and does not run CQF
// Measure reports.
//
// # Role in the stack
//
// FHIRPath (pkg/fhirpath) reads fields inside a single resource with path
// expressions. CQL names reusable clinical logic in libraries, supports
// Patient context, retrieve, and clinical helpers such as AgeInYears().
// SDC questionnaires may use either language; wire both through
// sdc.ComposeExpressions.
//
// Typical consumers are pkg/sdc (populate, validate, render expression
// providers) and pkg/runtime (default CQLProvider when libraries can be
// loaded from the resource store or contained Questionnaire resources).
//
// # Supported subset
//
//   - library / using FHIR / include / context Patient
//   - define statements (named expressions)
//   - literals, identifiers, arithmetic, comparison, and/or/not
//   - if-then-else, is null / is not null
//   - FHIR property navigation (Patient.name.given); FHIRPath when Config.FHIRPath is set
//   - retrieve [ResourceType] when a Retriever is configured
//   - codesystem / valueset / code declarations
//   - retrieve and `in` filters against Coding/CodeableConcept (MemberOf when Config.Terminology is set)
//   - First, Last, Count, Exists, AgeInYears, ToString and related helpers
//
// Unsupported (return a clear error): define function, ELM-only libraries,
// CQL query syntax (from / with / without), related-context retrieve,
// Interval types and promotion, and CQF Measure evaluation. This is a
// Patient-context subset for SDC, not a CQL 1.5 engine.
//
// # Integration
//
// pkg/cql must not be imported by pkg/sdc. The SDC adapter in this package
// implements sdc.CQLProvider. Applications and pkg/runtime inject it with
// sdc.ComposeExpressions.
package cql
