package view

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// AuditRecord captures one view access event for the audit seam. It is
// intentionally smaller than store.AuditRecord and scoped to view execution.
type AuditRecord struct {
	ViewName   string            `json:"viewName"`
	Version    string            `json:"version,omitempty"`
	Actor      string            `json:"actor"`
	Subject    string            `json:"subject,omitempty"`
	Outcome    string            `json:"outcome"`
	Details    map[string]string `json:"details,omitempty"`
	Parameters map[string]any    `json:"parameters,omitempty"`
	Timestamp  time.Time         `json:"timestamp"`
}

// AuditLogger is the pluggable audit seam. It is invoked on both successful and
// denied executions. The package remains usable without an audit logger.
type AuditLogger interface {
	LogViewAccess(ctx context.Context, rec AuditRecord) error
}

// AuditLoggerFunc adapts a function to the AuditLogger interface.
type AuditLoggerFunc func(ctx context.Context, rec AuditRecord) error

// LogViewAccess implements AuditLogger.
func (f AuditLoggerFunc) LogViewAccess(ctx context.Context, rec AuditRecord) error {
	return f(ctx, rec)
}

// AuditStoreAdapter writes view audit records to a store.AuditStore. It is a
// convenience adapter that lets the view package use the existing persistence
// contract without requiring it.
type AuditStoreAdapter struct {
	Store store.AuditStore
	Now   func() time.Time
}

// LogViewAccess converts a view.AuditRecord to a store.AuditRecord and appends
// it. The action is always "execute-view".
func (a *AuditStoreAdapter) LogViewAccess(ctx context.Context, rec AuditRecord) error {
	if a.Store == nil {
		return nil
	}
	if a.Now == nil {
		a.Now = time.Now
	}
	return a.Store.Append(ctx, store.AuditRecord{
		ID:        newAuditID(a.Now()),
		Timestamp: rec.Timestamp,
		Actor:     rec.Actor,
		Action:    "execute-view",
		Outcome:   rec.Outcome,
		Details:   auditDetails(rec),
	})
}

func auditDetails(rec AuditRecord) map[string]string {
	details := make(map[string]string, len(rec.Details)+len(rec.Parameters)+4)
	for k, v := range rec.Details {
		details[k] = v
	}
	for k, v := range rec.Parameters {
		details[k] = paramString(v)
	}
	details["viewName"] = rec.ViewName
	details["viewVersion"] = rec.Version
	if rec.Subject != "" {
		details["subject"] = rec.Subject
	}
	return details
}

func paramString(v any) string {
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

func newAuditID(now time.Time) string {
	return "audit-view-" + now.Format(time.RFC3339Nano)
}
