package analytics

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
