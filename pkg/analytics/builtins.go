package analytics

import (
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/view"
)

// RegisterBuiltInViews registers the first-milestone view definitions into reg.
func RegisterBuiltInViews(reg *view.Registry, engine fhirpath.Engine) error {
	if reg == nil {
		return fmt.Errorf("analytics: registry is required")
	}
	if engine == nil {
		return fmt.Errorf("analytics: engine is required")
	}
	builtins := []func() []byte{
		view.PatientSummaryView,
		view.AppointmentView,
		view.ObservationView,
	}
	for _, builtin := range builtins {
		if _, err := reg.Register(builtin(), engine); err != nil {
			return fmt.Errorf("register built-in view: %w", err)
		}
	}
	return nil
}
