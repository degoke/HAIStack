package auth

import "errors"

var (
	// ErrDenied is returned when a policy decision is deny (or adapters need an error).
	ErrDenied = errors.New("auth: denied")

	// ErrInvalidPolicy is returned when a policy document fails validation or parsing.
	ErrInvalidPolicy = errors.New("auth: invalid policy")

	// ErrInvalidConfig is returned when Engine configuration is incomplete or invalid.
	ErrInvalidConfig = errors.New("auth: invalid config")

	// ErrPrincipalNotFound is returned when a principal id cannot be resolved.
	ErrPrincipalNotFound = errors.New("auth: principal not found")

	// ErrDeviceNotFound is returned when a device identity cannot be resolved.
	ErrDeviceNotFound = errors.New("auth: device not found")

	// ErrRoleNotFound is returned when a role name cannot be resolved.
	ErrRoleNotFound = errors.New("auth: role not found")

	// ErrMissingResolver is returned when an adapter needs an actor resolver but none is set.
	ErrMissingResolver = errors.New("auth: missing actor resolver")
)
