package main

import (
	"github.com/degoke/haistack/pkg/types"
	"github.com/degoke/haistack/research/internal/researchutil"
)

const (
	pipelineTenant = "tenant-research"
	pipelineActor  = "user-clinician"
	conversationID = "research-ai-pipeline-001"
)

type synthPatient struct {
	ID, Family, Given, Gender string
}

type synthObservation struct {
	ID, PatientID, Code, Display string
	Value                        float64
	Unit                         string
	Status                       string
}

func pipelinePatients() []synthPatient {
	return []synthPatient{
		{ID: "pat-rivera", Family: "Rivera", Given: "Ana", Gender: "female"},
		{ID: "pat-okonkwo", Family: "Okonkwo", Given: "Chidi", Gender: "male"},
		{ID: "pat-berg", Family: "Berg", Given: "Elsa", Gender: "female"},
	}
}

func pipelineObservations() []synthObservation {
	return []synthObservation{
		{ID: "obs-hr-1", PatientID: "pat-rivera", Code: "8867-4", Display: "Heart rate", Value: 72, Unit: "beats/min", Status: "final"},
		{ID: "obs-sbp-1", PatientID: "pat-rivera", Code: "8480-6", Display: "Systolic blood pressure", Value: 118, Unit: "mmHg", Status: "final"},
		{ID: "obs-temp-1", PatientID: "pat-okonkwo", Code: "8310-5", Display: "Body temperature", Value: 37.1, Unit: "Cel", Status: "final"},
		{ID: "obs-spo2-1", PatientID: "pat-okonkwo", Code: "2708-6", Display: "Oxygen saturation", Value: 98, Unit: "%", Status: "final"},
		{ID: "obs-hr-2", PatientID: "pat-berg", Code: "8867-4", Display: "Heart rate", Value: 64, Unit: "beats/min", Status: "final"},
		{ID: "obs-prelim", PatientID: "pat-berg", Code: "8867-4", Display: "Heart rate", Value: 90, Unit: "beats/min", Status: "preliminary"},
	}
}

const haiPatientProfileURL = "http://haistack.example.org/fhir/StructureDefinition/hai-patient"

func patientEnvelope(p synthPatient) (*types.ResourceEnvelope, error) {
	return researchutil.ParseResource("Patient", researchutil.MustJSON(map[string]any{
		"resourceType": "Patient",
		"id":           p.ID,
		"meta": map[string]any{
			"profile": []any{haiPatientProfileURL},
		},
		"identifier": []any{map[string]any{
			"system": "http://fhir.haistack.io/sid/mrn",
			"value":  "mrn-" + p.ID,
		}},
		"gender": p.Gender,
		"name": []any{map[string]any{
			"family": p.Family,
			"given":  []any{p.Given},
		}},
	}))
}

func observationEnvelope(o synthObservation) (*types.ResourceEnvelope, error) {
	return researchutil.ParseResource("Observation", researchutil.MustJSON(map[string]any{
		"resourceType": "Observation",
		"id":           o.ID,
		"status":       o.Status,
		"code": map[string]any{
			"text": o.Display,
			"coding": []any{map[string]any{
				"system":  "http://loinc.org",
				"code":    o.Code,
				"display": o.Display,
			}},
		},
		"subject": map[string]any{"reference": "Patient/" + o.PatientID},
		"valueQuantity": map[string]any{
			"value":  o.Value,
			"unit":   o.Unit,
			"system": "http://unitsofmeasure.org",
		},
	}))
}
