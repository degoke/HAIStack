package ai

import (
	"context"
	"time"

	"github.com/degoke/health-ai-stack/pkg/audit"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// AuditRecord captures one AI tool or model invocation for the audit seam.
type AuditRecord struct {
	Action         string            `json:"action,omitempty"`
	ToolName       string            `json:"toolName"`
	Actor          string            `json:"actor"`
	Tenant         string            `json:"tenant,omitempty"`
	Subject        string            `json:"subject,omitempty"`
	Outcome        string            `json:"outcome"`
	Details        map[string]string `json:"details,omitempty"`
	ConversationID string            `json:"conversationId,omitempty"`
	Timestamp      time.Time         `json:"timestamp"`
}

// CanonicalAction is the pkg/audit action for this record. InvokeModel sets
// audit.ActionInvokeModel; ExecuteTool sets audit.ActionExecuteTool. Empty
// Action defaults to execute-tool.
func (r AuditRecord) CanonicalAction() string {
	if r.Action != "" {
		return r.Action
	}
	return audit.ActionExecuteTool
}

// AuditLogger is the pluggable audit seam.
//
// LogToolAccess is invoked for ExecuteTool (success, denial, validation
// failure, approval-required). LogModelInvoke is invoked for InvokeModel.
// Custom implementations must implement both; do not assume every record
// arrives through LogToolAccess.
type AuditLogger interface {
	LogToolAccess(ctx context.Context, rec AuditRecord) error
	LogModelInvoke(ctx context.Context, rec AuditRecord) error
}

// AuditLoggerFunc adapts a function to AuditLogger. Both tool and model
// records are delivered to the function; model records have Action set to
// audit.ActionInvokeModel first.
type AuditLoggerFunc func(ctx context.Context, rec AuditRecord) error

// LogToolAccess implements AuditLogger.
func (f AuditLoggerFunc) LogToolAccess(ctx context.Context, rec AuditRecord) error {
	return f(ctx, rec)
}

// LogModelInvoke implements AuditLogger.
func (f AuditLoggerFunc) LogModelInvoke(ctx context.Context, rec AuditRecord) error {
	if rec.Action == "" {
		rec.Action = audit.ActionInvokeModel
	}
	return f(ctx, rec)
}

// AuditStoreAdapter writes AI audit records through pkg/audit into a
// store.AuditStore. Action naming is owned by pkg/audit: ExecuteTool maps to
// execute-tool, and InvokeModel maps to invoke-model.
type AuditStoreAdapter struct {
	Store store.AuditStore
	Now   func() time.Time
}

// LogToolAccess converts an ExecuteTool ai.AuditRecord to execute-tool.
func (a *AuditStoreAdapter) LogToolAccess(ctx context.Context, rec AuditRecord) error {
	if a == nil || a.Store == nil {
		return nil
	}
	return audit.LogAIToolCall(ctx, &audit.StoreAdapter{Store: a.Store, Now: a.Now}, toolCallEvent(rec))
}

// LogModelInvoke converts an InvokeModel ai.AuditRecord to invoke-model.
func (a *AuditStoreAdapter) LogModelInvoke(ctx context.Context, rec AuditRecord) error {
	if a == nil || a.Store == nil {
		return nil
	}
	if rec.Action == "" {
		rec.Action = audit.ActionInvokeModel
	}
	return audit.LogAIModelInvoke(ctx, &audit.StoreAdapter{Store: a.Store, Now: a.Now}, toolCallEvent(rec))
}

func toolCallEvent(rec AuditRecord) audit.AIToolCallEvent {
	return audit.AIToolCallEvent{
		Actor:          rec.Actor,
		Tenant:         rec.Tenant,
		Subject:        rec.Subject,
		ToolName:       rec.ToolName,
		Outcome:        rec.Outcome,
		ConversationID: rec.ConversationID,
		Details:        rec.Details,
		Timestamp:      rec.Timestamp,
	}
}
