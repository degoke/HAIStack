package factories

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// PatientOption configures Patient factory output.
type PatientOption func(*patientConfig)

type patientConfig struct {
	id        string
	versionID string
	family    string
	given     string
	gender    string
	telecom   string
	updated   time.Time
	meta      map[string]any
}

// WithPatientID sets the patient id.
func WithPatientID(id string) PatientOption {
	return func(c *patientConfig) { c.id = id }
}

// WithVersionID sets the envelope version id.
func WithVersionID(versionID string) PatientOption {
	return func(c *patientConfig) { c.versionID = versionID }
}

// WithFamilyName sets the family name.
func WithFamilyName(family string) PatientOption {
	return func(c *patientConfig) { c.family = family }
}

// WithGivenName sets the given name.
func WithGivenName(given string) PatientOption {
	return func(c *patientConfig) { c.given = given }
}

// WithGender sets the patient gender.
func WithGender(gender string) PatientOption {
	return func(c *patientConfig) { c.gender = gender }
}

// WithTelecom sets a phone telecom value.
func WithTelecom(value string) PatientOption {
	return func(c *patientConfig) { c.telecom = value }
}

// WithLastUpdated sets the envelope last-updated timestamp.
func WithLastUpdated(t time.Time) PatientOption {
	return func(c *patientConfig) { c.updated = t }
}

// WithPatientMeta adds arbitrary FHIR meta fields. WithVersionID and
// WithLastUpdated take precedence for their respective reserved fields.
func WithPatientMeta(meta map[string]any) PatientOption {
	return func(c *patientConfig) { c.meta = cloneMeta(meta) }
}

// NewPatient builds a normalized Patient envelope. Invalid shapes return an error.
func NewPatient(opts ...PatientOption) (*types.ResourceEnvelope, error) {
	cfg := patientConfig{
		id:     "pat-1",
		family: "Doe",
		given:  "Jane",
		gender: "female",
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.id == "" {
		return nil, fmt.Errorf("factories.NewPatient: id is required")
	}

	obj := map[string]any{
		"resourceType": "Patient",
		"id":           cfg.id,
		"gender":       cfg.gender,
		"name":         []any{map[string]any{"given": []any{cfg.given}, "family": cfg.family}},
	}
	if cfg.telecom != "" {
		obj["telecom"] = []any{map[string]any{"system": "phone", "value": cfg.telecom}}
	}
	if len(cfg.meta) > 0 || cfg.versionID != "" || !cfg.updated.IsZero() {
		meta := cloneMeta(cfg.meta)
		if meta == nil {
			meta = map[string]any{}
		}
		if cfg.versionID != "" {
			meta["versionId"] = cfg.versionID
		}
		if !cfg.updated.IsZero() {
			meta["lastUpdated"] = cfg.updated.UTC().Format(time.RFC3339Nano)
		}
		obj["meta"] = meta
	}

	data, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("factories.NewPatient: %w", err)
	}
	return types.NewJSONCodec().ParseJSON("Patient", data)
}

// AppointmentOption configures Appointment factory output.
type AppointmentOption func(*appointmentConfig)

type appointmentConfig struct {
	id          string
	status      string
	patientRef  string
	description string
	start       string
	meta        map[string]any
}

// WithAppointmentID sets the appointment id.
func WithAppointmentID(id string) AppointmentOption {
	return func(c *appointmentConfig) { c.id = id }
}

// WithAppointmentStatus sets the appointment status.
func WithAppointmentStatus(status string) AppointmentOption {
	return func(c *appointmentConfig) { c.status = status }
}

// WithPatientReference sets the participant patient reference id.
func WithPatientReference(patientID string) AppointmentOption {
	return func(c *appointmentConfig) { c.patientRef = patientID }
}

// WithDescription sets the appointment description.
func WithDescription(desc string) AppointmentOption {
	return func(c *appointmentConfig) { c.description = desc }
}

// WithStart sets the appointment start time (ISO-8601).
func WithStart(start string) AppointmentOption {
	return func(c *appointmentConfig) { c.start = start }
}

// WithAppointmentMeta adds arbitrary FHIR meta fields.
func WithAppointmentMeta(meta map[string]any) AppointmentOption {
	return func(c *appointmentConfig) { c.meta = cloneMeta(meta) }
}

// NewAppointment builds a normalized Appointment envelope.
func NewAppointment(opts ...AppointmentOption) (*types.ResourceEnvelope, error) {
	cfg := appointmentConfig{
		id:          "appt-1",
		status:      "booked",
		patientRef:  "pat-1",
		description: "Visit",
		start:       "2024-06-15T09:00:00Z",
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.id == "" {
		return nil, fmt.Errorf("factories.NewAppointment: id is required")
	}
	if cfg.patientRef == "" {
		return nil, fmt.Errorf("factories.NewAppointment: patient reference is required")
	}

	obj := map[string]any{
		"resourceType": "Appointment",
		"id":           cfg.id,
		"status":       cfg.status,
		"description":  cfg.description,
		"start":        cfg.start,
		"participant": []any{
			map[string]any{
				"actor":  map[string]any{"reference": "Patient/" + cfg.patientRef},
				"status": "accepted",
			},
		},
	}
	if len(cfg.meta) > 0 {
		obj["meta"] = cloneMeta(cfg.meta)
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("factories.NewAppointment: %w", err)
	}
	return types.NewJSONCodec().ParseJSON("Appointment", data)
}

// ObservationOption configures Observation factory output.
type ObservationOption func(*observationConfig)

type observationConfig struct {
	id         string
	status     string
	patientRef string
	code       string
	value      float64
	unit       string
	meta       map[string]any
}

// WithObservationID sets the observation id.
func WithObservationID(id string) ObservationOption {
	return func(c *observationConfig) { c.id = id }
}

// WithObservationStatus sets the observation status.
func WithObservationStatus(status string) ObservationOption {
	return func(c *observationConfig) { c.status = status }
}

// WithSubjectReference sets the subject patient reference id.
func WithSubjectReference(patientID string) ObservationOption {
	return func(c *observationConfig) { c.patientRef = patientID }
}

// WithCodeText sets the observation code text.
func WithCodeText(code string) ObservationOption {
	return func(c *observationConfig) { c.code = code }
}

// WithQuantityValue sets the valueQuantity value and unit.
func WithQuantityValue(value float64, unit string) ObservationOption {
	return func(c *observationConfig) {
		c.value = value
		c.unit = unit
	}
}

// WithObservationMeta adds arbitrary FHIR meta fields.
func WithObservationMeta(meta map[string]any) ObservationOption {
	return func(c *observationConfig) { c.meta = cloneMeta(meta) }
}

// NewObservation builds a normalized Observation envelope.
func NewObservation(opts ...ObservationOption) (*types.ResourceEnvelope, error) {
	cfg := observationConfig{
		id:         "obs-1",
		status:     "final",
		patientRef: "pat-1",
		code:       "Body temperature",
		value:      37.0,
		unit:       "Cel",
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.id == "" {
		return nil, fmt.Errorf("factories.NewObservation: id is required")
	}
	if cfg.patientRef == "" {
		return nil, fmt.Errorf("factories.NewObservation: subject reference is required")
	}

	obj := map[string]any{
		"resourceType": "Observation",
		"id":           cfg.id,
		"status":       cfg.status,
		"code":         map[string]any{"text": cfg.code},
		"subject":      map[string]any{"reference": "Patient/" + cfg.patientRef},
		"valueQuantity": map[string]any{
			"value": cfg.value,
			"unit":  cfg.unit,
		},
	}
	if len(cfg.meta) > 0 {
		obj["meta"] = cloneMeta(cfg.meta)
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("factories.NewObservation: %w", err)
	}
	return types.NewJSONCodec().ParseJSON("Observation", data)
}

func cloneMeta(meta map[string]any) map[string]any {
	if meta == nil {
		return nil
	}
	out := make(map[string]any, len(meta))
	for key, value := range meta {
		out[key] = value
	}
	return out
}
