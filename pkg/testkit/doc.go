// Package testkit implements haistack-testkit, the shared importable test-support
// library for Health AI Stack.
//
// haistack-testkit consolidates FHIR resource fixtures, in-memory store fakes, sync and
// conflict scenario runners, OperationOutcome golden helpers, FHIRPath assertion wrappers,
// and AI executor harnesses that were previously duplicated across package-local _test.go
// files (for example pkg/sync/helpers_test.go, pkg/store/store_test.go, pkg/ai/fixtures_test.go).
//
// # Test-only boundary
//
// This tree is for tests only. Production packages must not import pkg/testkit or any
// subpackage. Runtime persistence belongs in pkg/sqlite, pkg/postgres, and future
// pkg/store/memory adapters — not here.
//
// # Subpackages
//
//   - fixtures — stable named presets (Patient, Appointment, Observation) as *types.ResourceEnvelope
//   - factories — option-based envelope builders for MVP resource types
//   - storetest — in-memory store.ResourceStore, HistoryStore, EventStore, and related fakes
//   - synctest — fake sync.Hub, device nodes, and push/pull scenario runners
//   - conflicttest — conflict detection and resolution scenarios on top of synctest
//   - golden — canonical OperationOutcome JSON comparison (inline/embedded goldens)
//   - fhirpathtest — FHIRPath evaluation and assertion helpers over fixtures
//   - aitest — reusable ai.Executor harness with optional search, views, and core wiring
//     (including shared resource/search stores and approval/model fakes)
//
// # Typical usage
//
// Import only the subpackages a test needs:
//
//	import (
//	    "context"
//	    "testing"
//
//	    "github.com/degoke/health-ai-stack/pkg/testkit/fixtures"
//	    "github.com/degoke/health-ai-stack/pkg/testkit/synctest"
//	)
//
//	func TestOfflinePatientSync(t *testing.T) {
//	    ctx := context.Background()
//	    scenario := synctest.NewScenario("tenant-a", synctest.FixedClock(synctest.At(2026, 7, 6, 12, 0, 0)))
//	    patient := fixtures.PatientJane(t)
//	    result, err := synctest.OfflineCreateAndSync(ctx, scenario, patient)
//	    if err != nil {
//	        t.Fatal(err)
//	    }
//	    _ = result.PushSummary
//	    _ = result.PullSummary
//	}
//
// # Migration
//
// Existing package-local test helpers are intentionally left in place. Downstream tests
// can migrate incrementally once pkg/testkit APIs prove stable. No production package
// should depend on this tree.
//
// # Dependencies
//
// pkg/testkit may depend on pkg/types, pkg/store, pkg/sync, pkg/conflict, pkg/fhirpath,
// pkg/ai, pkg/search, pkg/view, pkg/core, pkg/validate, pkg/registry, pkg/proto, and
// pkg/jobs. There is no reverse dependency from production code back into pkg/testkit.
//
// # File layout
//
//   - doc.go, keys.go, README.md — package overview
//   - fixtures/   — named FHIR envelope presets
//   - factories/  — option-based envelope builders
//   - storetest/  — in-memory store implementations and Backend bundle
//   - synctest/   — MemHub, Device, Scenario runners
//   - conflicttest/ — conflict scenario utilities
//   - golden/     — OperationOutcome comparison helpers
//   - fhirpathtest/ — FHIRPath test assertions
//   - aitest/     — AI executor harness and fakes
package testkit
