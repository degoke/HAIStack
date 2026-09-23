package ai

import "context"

// DefaultRedactedValue is the placeholder written when a PHI field is removed.
const DefaultRedactedValue = "[redacted]"

// DeidentifyRequest carries output data to scrub before returning to a model.
type DeidentifyRequest struct {
	ToolName     string
	ResourceType string
	ViewName     string
	ViewVersion  string
	Data         any
}

// Deidentifier is the optional output scrubbing seam. FHIRDeidentifier handles
// read_fhir_resource, search_fhir_resources, and run_view only; other tool
// names return ErrUnsupportedDeidentifyTool unless you use PassThroughDeidentifier
// or a custom DeidentifierFunc. Executor wires FHIRDeidentifier by default.
type Deidentifier interface {
	Deidentify(ctx context.Context, req DeidentifyRequest) (any, []string, error)
}

// PassThroughDeidentifier returns data unchanged with no redactions.
type PassThroughDeidentifier struct{}

// Deidentify implements Deidentifier.
func (PassThroughDeidentifier) Deidentify(_ context.Context, req DeidentifyRequest) (any, []string, error) {
	return req.Data, nil, nil
}

// DeidentifierFunc adapts a function to the Deidentifier interface.
type DeidentifierFunc func(ctx context.Context, req DeidentifyRequest) (any, []string, error)

// Deidentify implements Deidentifier.
func (f DeidentifierFunc) Deidentify(ctx context.Context, req DeidentifyRequest) (any, []string, error) {
	return f(ctx, req)
}
