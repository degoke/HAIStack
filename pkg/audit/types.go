package audit

import "time"

// Outcome values used across package emit helpers.
const (
	OutcomeSuccess          = "success"
	OutcomeDenied           = "denied"
	OutcomeError            = "error"
	OutcomeValidationFailed = "validation-failed"
	OutcomeApprovalRequired = "approval-required"
	OutcomeAllow            = "allow"
	OutcomeDeny             = "deny"
)

// Canonical action names. Producers should prefer these over free-form strings.
const (
	ActionResourceRead  = "resource.read"
	ActionResourceWrite = "resource.write"

	ActionExecuteTool = "execute-tool"
	ActionExecuteView = "execute-view"

	ActionAuthAllow = "auth.allow"
	ActionAuthDeny  = "auth.deny"

	ActionSyncAccepted        = "sync.accepted"
	ActionSyncRejected        = "sync.rejected"
	ActionSyncConflicted      = "sync.conflicted"
	ActionDevicePushed        = "sync.device_pushed"
	ActionDevicePulled        = "sync.device_pulled"
	ActionConflictAutoMerged  = "conflict.auto_merged"
	ActionConflictNeedsReview = "conflict.needs_review"

	ActionExport     = "export"
	ActionBlobAccess = "blob.access"
)

// Event is the canonical audit event model used across the stack.
type Event struct {
	ID           string            `json:"id"`
	Timestamp    time.Time         `json:"timestamp"`
	Actor        string            `json:"actor"`
	Tenant       string            `json:"tenant,omitempty"`
	Subject      string            `json:"subject,omitempty"`
	Action       string            `json:"action"`
	Outcome      string            `json:"outcome,omitempty"`
	ResourceType string            `json:"resourceType,omitempty"`
	ResourceID   string            `json:"resourceId,omitempty"`
	ViewName     string            `json:"viewName,omitempty"`
	ToolName     string            `json:"toolName,omitempty"`
	ModuleName   string            `json:"moduleName,omitempty"`
	BlobKey      string            `json:"blobKey,omitempty"`
	Details      map[string]string `json:"details,omitempty"`
}

// Query selects audit events. Filters that store.AuditQuery cannot express
// (action, outcome, tenant) are applied in-memory after List when using
// StoreAdapter.ListEvents.
type Query struct {
	ResourceType string    `json:"resourceType,omitempty"`
	ResourceID   string    `json:"resourceId,omitempty"`
	Actor        string    `json:"actor,omitempty"`
	Action       string    `json:"action,omitempty"`
	Outcome      string    `json:"outcome,omitempty"`
	Tenant       string    `json:"tenant,omitempty"`
	After        time.Time `json:"after,omitempty"`
	Before       time.Time `json:"before,omitempty"`
	Limit        int       `json:"limit,omitempty"`
}
