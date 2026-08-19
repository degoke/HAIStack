package analytics

import (
	"bytes"

	"github.com/degoke/health-ai-stack/pkg/view"
)

// Supported view names for the first analytics milestone.
const (
	ViewPatientSummary = "patient_summary_view"
	ViewAppointment    = "appointment_view"
	ViewObservation    = "observation_view"
)

// SupportedViews lists the built-in views supported in v1.
var SupportedViews = []string{
	ViewPatientSummary,
	ViewAppointment,
	ViewObservation,
}

// IsSupportedView reports whether name is in the first-milestone view set.
func IsSupportedView(name string) bool {
	for _, v := range SupportedViews {
		if v == name {
			return true
		}
	}
	return false
}

func packagedViewDefinition(name string) []byte {
	switch name {
	case ViewPatientSummary:
		return view.PatientSummaryView()
	case ViewAppointment:
		return view.AppointmentView()
	case ViewObservation:
		return view.ObservationView()
	default:
		return nil
	}
}

func isPackagedView(spec *view.ViewSpec) bool {
	if spec == nil || spec.Version != "1.0.0" {
		return false
	}
	definition := packagedViewDefinition(spec.Name)
	return len(definition) > 0 && bytes.Equal(spec.Raw, definition)
}
