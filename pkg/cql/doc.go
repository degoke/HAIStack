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
//   - FHIR property navigation (Patient.name.given)
//   - retrieve [ResourceType] when a Retriever is configured
//   - First, Last, Count, Exists, AgeInYears, ToString and related helpers
//
// Unsupported (return a clear error): define function, ELM-only libraries,
// related-context retrieve, Interval promotion, terminology membership,
// and CQF Measure evaluation.
//
// # Integration
//
// pkg/cql must not be imported by pkg/sdc. The SDC adapter in this package
// implements sdc.CQLProvider. Applications and pkg/runtime inject it with
// sdc.ComposeExpressions.
package cql
