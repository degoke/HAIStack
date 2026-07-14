package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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

// AuditStoreAdapter writes AI audit records to a store.AuditStore.
type AuditStoreAdapter struct {
	Store store.AuditStore
	Now   func() time.Time
}

// LogToolAccess converts an ai.AuditRecord to a store.AuditRecord and appends
// it. The action is always "execute-tool".
func (a *AuditStoreAdapter) LogToolAccess(ctx context.Context, rec AuditRecord) error {
	if a.Store == nil {
		return nil
	}
	now := a.Now
	if now == nil {
		now = time.Now
	}
	details := make(map[string]string, len(rec.Details)+2)
	for k, v := range rec.Details {
		details[k] = v
	}
	details["toolName"] = rec.ToolName
	if rec.Subject != "" {
		details["subject"] = rec.Subject
	}
	if rec.ConversationID != "" {
		details["conversationId"] = rec.ConversationID
	}
	return a.Store.Append(ctx, store.AuditRecord{
		ID:        "audit-ai-" + now().Format(time.RFC3339Nano),
		Timestamp: rec.Timestamp,
		Actor:     rec.Actor,
		Action:    "execute-tool",
		Outcome:   rec.Outcome,
		Details:   details,
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
