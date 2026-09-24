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

	// ErrMissingDeidentifier is returned when a nil FHIRDeidentifier is used or
	// de-identification is invoked without a configured implementation. Executor
	// defaults Config.Deidentify to DefaultDeidentifier when unset.
	ErrMissingDeidentifier = errors.New("ai: missing deidentifier")

	// ErrUnsupportedDeidentifyTool is returned when FHIRDeidentifier receives a
	// ToolName it does not handle. Custom tools must use DeidentifierFunc or
	// PassThroughDeidentifier explicitly.
	ErrUnsupportedDeidentifyTool = errors.New("ai: unsupported tool for FHIR de-identification")

	// ErrMissingAudit is returned when audit is required but not configured.
	ErrMissingAudit = errors.New("ai: missing audit logger")

	// ErrMissingConversationID is returned when deployment policy requires a
	// correlation ID but the request does not provide one.
	ErrMissingConversationID = errors.New("ai: missing conversation id")

	// ErrAuditFailed is returned when required audit persistence fails.
	ErrAuditFailed = errors.New("ai: audit persistence failed")

	// ErrApprovalTokenRequired is returned when an approval provider approves a
	// write without returning a verifiable token.
	ErrApprovalTokenRequired = errors.New("ai: approval token required")

	// ErrApprovalTokenInvalid is returned when an approval token cannot be
	// verified for the exact requested write.
	ErrApprovalTokenInvalid = errors.New("ai: invalid approval token")

	// ErrMissingApprovalStore is returned when a required approval cannot be
	// verified because no approval store is configured.
	ErrMissingApprovalStore = errors.New("ai: missing approval store")

	// ErrUngroundedAnswer is returned when strict grounding checks fail (missing tool evidence or uncited refs).
	ErrUngroundedAnswer = errors.New("ai: ungrounded answer")

	// ErrCommitNotConfirmed is returned when host confirmation is required but not granted for CommitWrite.
	ErrCommitNotConfirmed = errors.New("ai: commit write not confirmed")
)
