package ai

import (
	"fmt"
	"sync"
)

// ToolSpec describes a registered tool. Generic tools are built into the Executor;
// the registry holds convenience wrappers and custom tools.
type ToolSpec struct {
	Name        string
	Description string
	Delegate    string
	MapInput    func(input map[string]any) (map[string]any, error)
}

// Registry stores named tools for runtime lookup. It is safe for concurrent use.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]*ToolSpec
}

// NewRegistry returns a tool registry with built-in convenience wrappers
// pre-registered.
func NewRegistry() *Registry {
	r := &Registry{tools: make(map[string]*ToolSpec)}
	r.registerBuiltins()
	return r
}

func (r *Registry) registerBuiltins() {
	_ = r.register(&ToolSpec{
		Name:        ToolGetPatientSummary,
		Description: "Patient summary via patient_summary_view",
		Delegate:    ToolRunView,
		MapInput: func(input map[string]any) (map[string]any, error) {
			out := map[string]any{
				"viewName": "patient_summary_view",
				"version":  "1.0.0",
			}
			if limit, ok := input["limit"]; ok {
				out["limit"] = limit
			}
			if offset, ok := input["offset"]; ok {
				out["offset"] = offset
			}
			if params, ok := input["parameters"]; ok {
				out["parameters"] = params
			}
			return out, nil
		},
	})
	_ = r.register(&ToolSpec{
		Name:        ToolGetUpcomingAppointments,
		Description: "Appointments via appointment_view",
		Delegate:    ToolRunView,
		MapInput: func(input map[string]any) (map[string]any, error) {
			out := map[string]any{
				"viewName": "appointment_view",
				"version":  "1.0.0",
			}
			if limit, ok := input["limit"]; ok {
				out["limit"] = limit
			}
			if offset, ok := input["offset"]; ok {
				out["offset"] = offset
			}
			return out, nil
		},
	})
	_ = r.register(&ToolSpec{
		Name:        ToolSearchPatientByPhone,
		Description: "Search patients by phone number",
		Delegate:    ToolSearchFhirResources,
		MapInput: func(input map[string]any) (map[string]any, error) {
			phone, ok := input["phone"].(string)
			if !ok || phone == "" {
				return nil, fmt.Errorf("%w: phone is required", ErrInvalidInput)
			}
			out := map[string]any{
				"resourceType": "Patient",
				"params": map[string][]string{
					"telecom": {phone},
				},
			}
			if count, ok := input["count"]; ok {
				out["count"] = count
			}
			if offset, ok := input["offset"]; ok {
				out["offset"] = offset
			}
			return out, nil
		},
	})
}

func (r *Registry) register(spec *ToolSpec) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[spec.Name]; ok {
		return fmt.Errorf("%w: %s", ErrToolAlreadyRegistered, spec.Name)
	}
	r.tools[spec.Name] = spec
	return nil
}

// Register adds a custom tool to the registry.
func (r *Registry) Register(spec ToolSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("%w: tool name is required", ErrInvalidInput)
	}
	if spec.Delegate == "" {
		return fmt.Errorf("%w: delegate tool is required", ErrInvalidInput)
	}
	if spec.MapInput == nil {
		return fmt.Errorf("%w: MapInput is required", ErrInvalidInput)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[spec.Name]; ok {
		return fmt.Errorf("%w: %s", ErrToolAlreadyRegistered, spec.Name)
	}
	cp := spec
	r.tools[spec.Name] = &cp
	return nil
}

// Resolve returns a registered tool by name. Returns ErrToolNotFound for unknown
// names and built-in generic tools (those are handled directly by the Executor).
func (r *Registry) Resolve(name string) (*ToolSpec, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	return spec, nil
}

// List returns all registered tools.
func (r *Registry) List() []*ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*ToolSpec, 0, len(r.tools))
	for _, spec := range r.tools {
		out = append(out, spec)
	}
	return out
}

// IsGeneric reports whether name is a built-in generic tool.
func IsGeneric(name string) bool {
	switch name {
	case ToolReadFhirResource, ToolSearchFhirResources, ToolRunView, ToolWriteFhirResource:
		return true
	default:
		return false
	}
}
