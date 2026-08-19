package ai

import (
	"time"
)

// Tool name constants for the v1 generic operations.
const (
	ToolReadFhirResource    = "read_fhir_resource"
	ToolSearchFhirResources = "search_fhir_resources"
	ToolRunView             = "run_view"
	ToolWriteFhirResource   = "write_fhir_resource"
)

// Convenience tool names built on the generic core.
const (
	ToolGetPatientSummary       = "get_patient_summary"
	ToolGetUpcomingAppointments = "get_upcoming_appointments"
	ToolSearchPatientByPhone    = "search_patient_by_phone"
)

// ToolRequest carries runtime parameters for one tool invocation.
type ToolRequest struct {
	ToolName       string
	Actor          string
	TenantID       string
	Subject        string
	Input          map[string]any
	ConversationID string
	ModelHint      string
	ApprovalToken  string
}

// ToolResult is the structured output of a tool invocation.
type ToolResult struct {
	ToolName         string
	Data             any
	Context          string
	Citations        []Citation
	AuditMeta        AuditMeta
	ApprovalRequired bool
	ApprovalToken    string
	Redactions       []string
}

// Citation attaches provenance to tool output for model grounding.
type Citation struct {
	Kind   string            `json:"kind"`
	Ref    string            `json:"ref,omitempty"`
	Detail map[string]string `json:"detail,omitempty"`
}

// AuditMeta captures execution-side metadata returned with tool results.
type AuditMeta struct {
	ExecutedAt time.Time     `json:"executedAt"`
	Duration   time.Duration `json:"duration"`
	Outcome    string        `json:"outcome"`
}

// ReadInput is the typed input for read_fhir_resource.
type ReadInput struct {
	ResourceType string
	ID           string
}

// SearchInput is the typed input for search_fhir_resources.
type SearchInput struct {
	ResourceType string
	Params       map[string][]string
	Count        int
	Offset       int
}

// ViewInput is the typed input for run_view.
type ViewInput struct {
	ViewName   string
	Version    string
	Parameters map[string]any
	Limit      int
	Offset     int
}

// WriteInput is the typed input for write_fhir_resource.
type WriteInput struct {
	Operation    string
	ResourceType string
	ID           string
	Fields       map[string]any
}
