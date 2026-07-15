package fhirpathtest_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/testkit/fhirpathtest"
	"github.com/degoke/health-ai-stack/pkg/testkit/fixtures"
)

func TestPatientNameExpression(t *testing.T) {
	eng := fhirpathtest.DefaultEngine(t)
	patient := fixtures.PatientJane(t)
	fhirpathtest.AssertString(t, eng, patient, "Patient.name.family", "Doe")
	fhirpathtest.AssertString(t, eng, patient, "Patient.name.given.first()", "Jane")
}

func TestObservationSubject(t *testing.T) {
	eng := fhirpathtest.DefaultEngine(t)
	obs := fixtures.ObservationVitals(t)
	fhirpathtest.AssertString(t, eng, obs, "Observation.subject.reference", "Patient/pat-jane")
}

func TestAssertEmpty(t *testing.T) {
	eng := fhirpathtest.DefaultEngine(t)
	patient := fixtures.PatientJane(t)
	fhirpathtest.AssertEmpty(t, eng, patient, "Patient.address")
}
