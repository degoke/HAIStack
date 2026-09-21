// Package cql implements a Clinical Quality Language (CQL) 1.5 engine for
// HAIStack SDC questionnaires and CQF Measure evaluation.
//
// The engine compiles text/cql libraries and evaluates them against Patient
// or Unfiltered context. FHIR Library resources that contain only ELM remain
// unsupported; CQL source is required.
//
// # Role in the stack
//
// FHIRPath (pkg/fhirpath) reads fields inside a single resource with path
// expressions. CQL names reusable clinical logic in libraries, supports
// Patient context, retrieve, Interval/Quantity, queries, and functions.
// CQF Measure resources reference those libraries; EvaluateMeasure produces
// a MeasureReport (individual, summary, or subject-list).
// SDC questionnaires may use either language; wire both through
// sdc.ComposeExpressions.
//
// Typical consumers are pkg/sdc (populate, validate, render expression
// providers), pkg/http (Measure/$evaluate-measure), and pkg/runtime (default
// CQLProvider and measure service when libraries can be loaded from the
// resource store or contained Questionnaire resources).
//
// # CQL 1.5 coverage
//
//   - library / using FHIR / include / context Patient | Unfiltered
//   - define statements and define function / define fluent function
//   - parameter declarations with optional defaults (including Measurement Period)
//   - literals, identifiers, arithmetic, comparison, and/or/xor/not/implies
//   - if-then-else, case-when-else, is null / is not null, as Type
//   - FHIR property navigation (Patient.name.given); FHIRPath when Config.FHIRPath is set
//   - retrieve [ResourceType] (optional terminology) and related-context aliases
//   - queries: from / where / return / sort / with / without / let
//   - Interval values and operators (in, contains, during, includes, overlaps, starts, ends, before, after)
//   - Quantity literals (5 'mg', 1 year) and duration in years/months/days
//   - codesystem / valueset / code declarations
//   - retrieve and `in` filters against Coding/CodeableConcept (MemberOf when Config.Terminology is set)
//   - First, Last, Count, Exists, AgeInYears, ToString, ToInterval, Min/Max/Sum and related helpers
//
// ELM-only libraries still return ErrUnsupported. This is a text/cql 1.5
// interpreter plus CQF Measure/$evaluate-measure, not an ELM runtime.
//
// # Integration
//
// pkg/cql must not be imported by pkg/sdc. The SDC adapter in this package
// implements sdc.CQLProvider. Applications and pkg/runtime inject it with
// sdc.ComposeExpressions. Measure/$evaluate-measure is exposed by pkg/http
// when a MeasureEvaluateService (default: http.CoreMeasureService) is wired.
package cql
