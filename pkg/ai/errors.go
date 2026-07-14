package ai

import "errors"

var (
	// ErrToolNotFound is returned when a tool name is not registered or recognized.
	ErrToolNotFound = errors.New("ai: tool not found")

	// ErrToolAlreadyRegistered is returned when registering a duplicate tool name.
	ErrToolAlreadyRegistered = errors.New("ai: tool already registered")

	// ErrInvalidInput is returned when tool input fails structural validation.
	ErrInvalidInput = errors.New("ai: invalid tool input")

	// ErrPolicyDenied is returned when the policy engine rejects an operation.
	ErrPolicyDenied = errors.New("ai: policy denied")

	// ErrUnauthorized is returned when authorization fails for a view execution.
	ErrUnauthorized = errors.New("ai: unauthorized")

	// ErrApprovalRequired is returned when a write requires human approval
	// before it can be committed.
	ErrApprovalRequired = errors.New("ai: approval required")

	// ErrValidationFailed is returned when a write fails structural validation.
	ErrValidationFailed = errors.New("ai: validation failed")

	// ErrMissingPolicy is returned when an Executor is configured without a
	// policy engine.
	ErrMissingPolicy = errors.New("ai: missing policy engine")

	// ErrMissingDependency is returned when a tool's backing service is not
	// configured on the Executor.
	ErrMissingDependency = errors.New("ai: missing backing dependency")
)
