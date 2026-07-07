package modules

import "errors"

var (
	// ErrManifestNotFound is returned when a module directory does not contain
	// a module.json manifest.
	ErrManifestNotFound = errors.New("module manifest not found")

	// ErrInvalidManifest is returned when a manifest fails validation.
	ErrInvalidManifest = errors.New("invalid module manifest")

	// ErrMissingDependency is returned when a required module dependency is not
	// installed.
	ErrMissingDependency = errors.New("missing module dependency")

	// ErrDependencyVersionMismatch is returned when an installed dependency is
	// below the required minimum version.
	ErrDependencyVersionMismatch = errors.New("module dependency version mismatch")

	// ErrCircularDependency is returned when a module's dependency graph contains
	// a cycle.
	ErrCircularDependency = errors.New("circular module dependency")

	// ErrAmbiguousModule is returned when a module name maps to multiple
	// conflicting versions or sources.
	ErrAmbiguousModule = errors.New("ambiguous module reference")

	// ErrDowngradeNotAllowed is returned when an upgrade would move to an older
	// or equal version.
	ErrDowngradeNotAllowed = errors.New("module downgrade not allowed")

	// ErrModuleNotFound is returned when an operation references a module that
	// is not installed.
	ErrModuleNotFound = errors.New("module not found")

	// ErrModuleInUse is returned when uninstall is blocked because another
	// installed module depends on the target.
	ErrModuleInUse = errors.New("module is required by another installed module")

	// ErrUpgradeWouldRemove is returned when an upgrade would remove
	// resources or definitions that were previously declared, requiring manual
	// action in v1.
	ErrUpgradeWouldRemove = errors.New("upgrade would remove previously declared resources or definitions")
)
