package view

// PatientSummaryView returns an example FHIR ViewDefinition for a flat patient
// summary. All selected columns resolve to JSON scalars in v1.
func PatientSummaryView() []byte {
	return []byte(`{
		"resourceType": "ViewDefinition",
		"name": "patient_summary_view",
		"version": "1.0.0",
		"status": "active",
		"description": "Flat patient summary with id, name, gender and phone",
		"resource": "Patient",
		"select": [{
			"column": [
				{"name": "id", "path": "Patient.id", "type": "string"},
				{"name": "given", "path": "Patient.name.first().given", "type": "string"},
				{"name": "family", "path": "Patient.name.first().family", "type": "string"},
				{"name": "gender", "path": "Patient.gender", "type": "string"},
				{"name": "phone", "path": "Patient.telecom.where(system = 'phone').value.first()", "type": "string"}
			]
		}],
		"permissions": ["read-patient-summary"]
	}`)
}

// UpcomingAppointmentsView returns an example FHIR ViewDefinition for booked
// appointments with a status/date filter.
func UpcomingAppointmentsView() []byte {
	return []byte(`{
		"resourceType": "ViewDefinition",
		"name": "upcoming_appointments_view",
		"version": "1.0.0",
		"status": "active",
		"description": "Booked appointments after a reference date",
		"resource": "Appointment",
		"select": [{
			"column": [
				{"name": "id", "path": "Appointment.id", "type": "string"},
				{"name": "status", "path": "Appointment.status", "type": "string"},
				{"name": "description", "path": "Appointment.description", "type": "string"},
				{"name": "patientRef", "path": "Appointment.participant.first().actor.reference", "type": "string"}
			]
		}],
		"where": [
			{"path": "Appointment.status = 'booked'", "description": "Only booked appointments"},
			{"path": "Appointment.start >= @2024-01-01", "description": "From 2024 onward"}
		]
	}`)
}

// RecentObservationsView returns an example FHIR ViewDefinition for final
// observations with scalar quantity extraction.
func RecentObservationsView() []byte {
	return []byte(`{
		"resourceType": "ViewDefinition",
		"name": "recent_observations_view",
		"version": "1.0.0",
		"status": "active",
		"description": "Final observations with code and quantity",
		"resource": "Observation",
		"select": [{
			"column": [
				{"name": "id", "path": "Observation.id", "type": "string"},
				{"name": "status", "path": "Observation.status", "type": "string"},
				{"name": "codeText", "path": "Observation.code.text", "type": "string"},
				{"name": "value", "path": "Observation.value.ofType(Quantity).value", "type": "decimal"},
				{"name": "unit", "path": "Observation.value.ofType(Quantity).unit", "type": "string"}
			]
		}],
		"where": [
			{"path": "Observation.status = 'final'", "description": "Only final results"}
		]
	}`)
}
