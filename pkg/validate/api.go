package validate

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/proto"
	"github.com/degoke/health-ai-stack/pkg/terminology"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// ReferencePolicy controls how Reference.reference values are validated.
type ReferencePolicy int

const (
	// ReferencePolicySyntactic validates reference string shape only (MVP default).
	ReferencePolicySyntactic ReferencePolicy = iota
)

// ResourceTypeRegistry reports whether a FHIR resource type is installed in the runtime.
type ResourceTypeRegistry interface {
	IsInstalled(resourceType string) bool
}

// MapResourceTypeRegistry is a set-based ResourceTypeRegistry.
type MapResourceTypeRegistry map[string]struct{}

// IsInstalled reports whether resourceType is present in the map.
func (m MapResourceTypeRegistry) IsInstalled(resourceType string) bool {
	_, ok := m[resourceType]
	return ok
}

// ValidateOptions configures a single Validate invocation.
type ValidateOptions struct {
	RequireID            bool
	ResourceTypeRegistry ResourceTypeRegistry
	ReferencePolicy      ReferencePolicy
	Terminology          terminology.Service
	TerminologyEnabled   bool
	TerminologyBindings  map[string]TerminologyBinding
}

type TerminologyBinding struct{ URL, Version, Strength string }

// ValidationIssue is one structured validation finding.
type ValidationIssue struct {
	Severity    string
	Code        string
	Diagnostics string
	Expression  []string
}

// ValidationResult is the structured output of Engine.Validate.
type ValidationResult struct {
	Valid  bool
	Issues []ValidationIssue
}

// Engine validates ResourceEnvelope values and returns structured findings.
type Engine interface {
	Validate(ctx context.Context, res *types.ResourceEnvelope, opts ValidateOptions) (*ValidationResult, error)
}

// Config configures a built-in validation Engine.
type Config struct {
	ProtoCodec         proto.ProtoCodec
	KnownResourceTypes map[string]struct{}
	InstalledTypes     ResourceTypeRegistry
	RequiredFields     map[string][]string
}
