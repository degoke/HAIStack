// Package aitest provides a reusable ai.Executor harness for tests, extracted
// from the pattern in pkg/ai/fixtures_test.go.
//
// NewHarness builds an ai.Executor with option-based subsystem wiring:
//
//   - SeedPatients, SeedAppointments — preload storetest.ResourceStore
//   - WithSearch, WithViews, WithCore, WithValidator — optional integrations
//   - AllowPatientRead, AllowPatientSearch, AllowPatientSummaryView, etc. — policy flags
//   - ApprovalGranted, WriteRequiresApproval — approval flow controls
//   - At, NewFixedClock — deterministic time helpers
//
// Exported fakes implement ai.AuditLogger, ai.ApprovalHook, ai.Deidentifier, and
// ai.ModelAdapter for direct assertion on tool access, approval calls, and de-id.
//
// Harness exposes Resources, Search, Views, Core, Policy, Audit, Approval, Deid,
// Model, Executor, and Clock so tests can assert both executor behavior and side effects.
package aitest
