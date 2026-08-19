// Package synctest provides fake sync infrastructure and reusable push/pull
// scenario runners for tests.
//
// # Components
//
//   - MemHub — in-process sync.Hub with idempotent push, optional stale-base
//     conflict simulation (SetStaleOnMismatch), and canonical event logging
//   - Device — one fake device node with storetest.Backend stores and a wired sync.Engine
//   - Scenario — two-device setup (device A + device B) sharing one MemHub
//   - FixedClock, At — deterministic time helpers
//
// # Scenario flows
//
// SeedLocalCreate / SeedLocalUpdate write offline state (resources, history, outbox).
// RunPushPull pushes from device A and pulls on device B. OfflineCreateAndSync
// combines seeding and sync for the common offline-create → cloud → second-device flow.
//
// ScenarioResult exposes push summary, pull summary, hub canonical events, device B
// resources, conflicts, and audit records for assertions.
//
// ReferenceResolved checks that a FHIR reference in a pulled envelope points to a
// resource that exists in the target device's local store. Paths support dotted
// object traversal and arbitrary array indices, such as participant[0].actor.
package synctest
