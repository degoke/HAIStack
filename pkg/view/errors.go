package view

import "errors"

var (
	// ErrInvalidViewDefinition is returned when a ViewDefinition JSON payload is
	// malformed or fails structural validation.
	ErrInvalidViewDefinition = errors.New("view: invalid ViewDefinition")

	// ErrUnsupportedFeature is returned when a ViewDefinition uses a feature
	// outside the v1 executable subset (joins, nested projections, forEach,
	// unionAll, multiple source resources, etc.).
	ErrUnsupportedFeature = errors.New("view: unsupported ViewDefinition feature")

	// ErrViewNotFound is returned when a named/versioned view is not registered.
	ErrViewNotFound = errors.New("view: view not found")

	// ErrViewAlreadyRegistered is returned when registering a view whose name and
	// version already exist in the registry.
	ErrViewAlreadyRegistered = errors.New("view: view already registered")

	// ErrUnauthorized is returned when the configured authorizer rejects an
	// execution request.
	ErrUnauthorized = errors.New("view: unauthorized")

	// ErrRowEncoding is returned when a column result cannot be normalized into a
	// JSON-safe value.
	ErrRowEncoding = errors.New("view: row encoding failed")

	// ErrMissingEngine is returned when an Executor is configured without a
	// FHIRPath engine.
	ErrMissingEngine = errors.New("view: missing FHIRPath engine")

	// ErrMissingResourceStore is returned when an Executor is configured without
	// a resource store.
	ErrMissingResourceStore = errors.New("view: missing resource store")
)
