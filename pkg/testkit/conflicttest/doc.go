// Package conflicttest provides conflict scenario runners built on pkg/sync and
// pkg/conflict for tests.
//
// Scenario wires two synctest.Device nodes, a synctest.MemHub with stale-base
// detection enabled, and conflict.NewDefaultEngine().
//
// Typical flows:
//
//   - Evaluate — run Detect and Merge for a local/base/current triple without sync I/O
//   - RunStaleBaseConflict — seed a hub-side current version and assert AckConflicted + conflict records
//   - RunTwoNodeStaleBaseConflict — push node B first, then assert node A conflicts on stale base
//   - RunAutoMergeResolution — process a sync.conflict_processing job, resubmit the merged update,
//     and assert the canonical event was created
//
// DefaultConcurrentPatientEdits returns a standard non-overlapping patient edit triple
// (telecom vs address) suitable for auto-merge scenarios.
//
// EnvelopeFromFields and LocalUpdate are low-level helpers for building conflict
// inputs without importing sync event types into unrelated tests.
package conflicttest
