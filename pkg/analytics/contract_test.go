package analytics_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/analytics"
	"github.com/degoke/health-ai-stack/pkg/view"
)

func TestContract_PatientSummaryViewSchema(t *testing.T) {
	assertViewContract(t, view.PatientSummaryView(), analytics.ViewPatientSummary, "Patient", []columnContract{
		{"id", "string"},
		{"given", "string"},
		{"family", "string"},
		{"gender", "string"},
		{"phone", "string"},
	})
}

func TestContract_AppointmentViewSchema(t *testing.T) {
	assertViewContract(t, view.AppointmentView(), analytics.ViewAppointment, "Appointment", []columnContract{
		{"id", "string"},
		{"status", "string"},
		{"description", "string"},
		{"patientRef", "string"},
	})
}

func TestContract_ObservationViewSchema(t *testing.T) {
	assertViewContract(t, view.ObservationView(), analytics.ViewObservation, "Observation", []columnContract{
		{"id", "string"},
		{"status", "string"},
		{"codeText", "string"},
		{"value", "decimal"},
		{"unit", "string"},
	})
}

type columnContract struct {
	name string
	typ  string
}

func assertViewContract(t *testing.T, def []byte, name, resourceType string, wantCols []columnContract) {
	t.Helper()
	spec, err := view.ParseDefinition(def, defaultEngine(t))
	if err != nil {
		t.Fatalf("ParseDefinition: %v", err)
	}
	if spec.Name != name {
		t.Fatalf("Name = %q, want %q", spec.Name, name)
	}
	if spec.Version != "1.0.0" {
		t.Fatalf("Version = %q, want 1.0.0", spec.Version)
	}
	if spec.ResourceType != resourceType {
		t.Fatalf("ResourceType = %q, want %q", spec.ResourceType, resourceType)
	}
	cols := spec.ColumnInfos()
	if len(cols) != len(wantCols) {
		t.Fatalf("len(columns) = %d, want %d", len(cols), len(wantCols))
	}
	for i, want := range wantCols {
		if cols[i].Name != want.name {
			t.Errorf("column[%d].Name = %q, want %q", i, cols[i].Name, want.name)
		}
		if cols[i].Type != want.typ {
			t.Errorf("column[%d].Type = %q, want %q", i, cols[i].Type, want.typ)
		}
	}
}
