package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/audit"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// AuditRecord captures one AI tool invocation for the audit seam.
type AuditRecord struct {
	ToolName       string            `json:"toolName"`
	Actor          string            `json:"actor"`
	Subject        string            `json:"subject,omitempty"`
	Outcome        string            `json:"outcome"`
	Details        map[string]string `json:"details,omitempty"`
	ConversationID string            `json:"conversationId,omitempty"`
	Timestamp      time.Time         `json:"timestamp"`
}

// AuditLogger is the pluggable audit seam. It is invoked on success, denial,
// validation failure, and approval-required outcomes.
type AuditLogger interface {
	LogToolAccess(ctx context.Context, rec AuditRecord) error
}

// AuditLoggerFunc adapts a function to the AuditLogger interface.
type AuditLoggerFunc func(ctx context.Context, rec AuditRecord) error

// LogToolAccess implements AuditLogger.
func (f AuditLoggerFunc) LogToolAccess(ctx context.Context, rec AuditRecord) error {
	return f(ctx, rec)
}

// AuditStoreAdapter writes AI audit records through pkg/audit into a
// store.AuditStore. Action naming is owned by pkg/audit (execute-tool).
type AuditStoreAdapter struct {
	Store store.AuditStore
	Now   func() time.Time
}

// LogToolAccess converts an ai.AuditRecord to a canonical audit event and
// appends it.
func (a *AuditStoreAdapter) LogToolAccess(ctx context.Context, rec AuditRecord) error {
	if a == nil || a.Store == nil {
		return nil
	}
	return audit.LogAIToolCall(ctx, &audit.StoreAdapter{Store: a.Store, Now: a.Now}, audit.AIToolCallEvent{
		Actor:          rec.Actor,
		Subject:        rec.Subject,
		ToolName:       rec.ToolName,
		Outcome:        rec.Outcome,
		ConversationID: rec.ConversationID,
		Details:        rec.Details,
		Timestamp:      rec.Timestamp,
	})
}

func auditDetailString(v any) string {
	if v == nil {
		return "null"
	}
	if s, ok := v.(string); ok {
		return s
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}
