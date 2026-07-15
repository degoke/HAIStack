// Package factories builds normalized *types.ResourceEnvelope values for MVP
// resource types using a small options API.
//
// Factories return errors on invalid shapes (for example empty id or missing
// patient reference) so tests fail fast without panics. Successful builds always
// route through types.JSONCodec.ParseJSON for canonical JSON and hashing.
//
// Supported builders:
//
//   - NewPatient(...PatientOption)
//   - NewAppointment(...AppointmentOption)
//   - NewObservation(...ObservationOption)
//
// Prefer fixtures for stable named scenarios; use factories when tests need
// parameterized ids, references, status, or timestamps.
package factories
