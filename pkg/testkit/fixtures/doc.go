// Package fixtures provides stable, named FHIR resource presets as normalized
// *types.ResourceEnvelope values for tests.
//
// All fixtures parse through types.JSONCodec (or proto.GoogleR4Codec via
// EnvelopeFromProtoJSON) so JSON, Hash, VersionID, and LastUpdated match
// production envelope conventions.
//
// Named presets:
//
//   - PatientJane, PatientJohn, SamplePatient, OfflinePatientCreate
//   - AppointmentBooked, AppointmentForPatient
//   - ObservationVitals, ObservationForPatient
//
// Use factories when a test needs to override ids, references, or fields without
// copying raw JSON blobs.
package fixtures
