package view

import (
	"fmt"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
)

// Registry stores named/versioned views in memory for runtime lookup. It is safe
// for concurrent use. Registering a view compiles its FHIRPath expressions
// immediately with the supplied engine so that invalid expressions are caught at
// registration time.
type Registry struct {
	mu    sync.RWMutex
	views map[string]*ViewSpec
}

// NewRegistry returns an empty in-memory view registry.
func NewRegistry() *Registry {
	return &Registry{
		views: make(map[string]*ViewSpec),
	}
}

// registryKey returns the stable lookup key for a name/version pair.
func registryKey(name, version string) string {
	return name + "|" + version
}

// Register parses, validates, and stores a ViewDefinition. The name and version
// are taken from the ViewDefinition itself. All FHIRPath expressions are compiled
// with engine to fail fast on invalid expressions. If a view with the same name
// and version is already registered, ErrViewAlreadyRegistered is returned.
func (r *Registry) Register(def []byte, engine fhirpath.Engine) (*ViewSpec, error) {
	spec, err := ParseDefinition(def, engine)
	if err != nil {
		return nil, err
	}
	key := registryKey(spec.Name, spec.Version)

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.views[key]; ok {
		return nil, fmt.Errorf("%w: %s", ErrViewAlreadyRegistered, key)
	}
	r.views[key] = spec
	return spec, nil
}

// Get returns a registered view by name and version. Returns ErrViewNotFound
// when no matching view exists.
func (r *Registry) Get(name, version string) (*ViewSpec, error) {
	key := registryKey(name, version)
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.views[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrViewNotFound, key)
	}
	return spec, nil
}

// Resolve returns a registered view by name and version. If version is empty,
// the only registered version with that name is returned. If multiple versions
// are registered for the name and version is empty, ErrViewNotFound is returned.
func (r *Registry) Resolve(name, version string) (*ViewSpec, error) {
	if version != "" {
		return r.Get(name, version)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var found *ViewSpec
	for key, spec := range r.views {
		if spec.Name != name {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("%w: multiple versions of %s registered; specify a version", ErrViewNotFound, name)
		}
		found = spec
		_ = key
	}
	if found == nil {
		return nil, fmt.Errorf("%w: %s", ErrViewNotFound, name)
	}
	return found, nil
}

// List returns all registered views. The returned slice is a copy; the
// underlying ViewSpec values are shared.
func (r *Registry) List() []*ViewSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*ViewSpec, 0, len(r.views))
	for _, spec := range r.views {
		out = append(out, spec)
	}
	return out
}

// Unregister removes a view from the registry. Returns ErrViewNotFound if the
// view was not registered.
func (r *Registry) Unregister(name, version string) error {
	key := registryKey(name, version)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.views[key]; !ok {
		return fmt.Errorf("%w: %s", ErrViewNotFound, key)
	}
	delete(r.views, key)
	return nil
}
