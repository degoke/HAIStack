package audit

import (
	"context"
	"time"
)

func emit(ctx context.Context, logger Logger, event Event) error {
	if logger == nil {
		return ErrNilLogger
	}
	return logger.Log(ctx, event)
}

// ResourceReadEvent describes a FHIR resource read for audit.
type ResourceReadEvent struct {
	Actor        string
	Tenant       string
	Subject      string
	ResourceType string
	ResourceID   string
	Outcome      string
	Details      map[string]string
	Timestamp    time.Time
	ID           string
}

// LogResourceRead emits a resource.read event.
func LogResourceRead(ctx context.Context, logger Logger, ev ResourceReadEvent) error {
	outcome := ev.Outcome
	if outcome == "" {
		outcome = OutcomeSuccess
	}
	return emit(ctx, logger, Event{
		ID:           ev.ID,
		Timestamp:    ev.Timestamp,
		Actor:        ev.Actor,
		Tenant:       ev.Tenant,
		Subject:      ev.Subject,
		Action:       ActionResourceRead,
		Outcome:      outcome,
		ResourceType: ev.ResourceType,
		ResourceID:   ev.ResourceID,
		Details:      ev.Details,
	})
}

// ResourceWriteEvent describes a FHIR resource write for audit.
type ResourceWriteEvent struct {
	Actor        string
	Tenant       string
	Subject      string
	ResourceType string
	ResourceID   string
	Operation    string
	Outcome      string
	Details      map[string]string
	Timestamp    time.Time
	ID           string
}

// LogResourceWrite emits a resource.write event.
func LogResourceWrite(ctx context.Context, logger Logger, ev ResourceWriteEvent) error {
	details := cloneDetails(ev.Details)
	if ev.Operation != "" {
		details["operation"] = ev.Operation
	}
	outcome := ev.Outcome
	if outcome == "" {
		outcome = OutcomeSuccess
	}
	return emit(ctx, logger, Event{
		ID:           ev.ID,
		Timestamp:    ev.Timestamp,
		Actor:        ev.Actor,
		Tenant:       ev.Tenant,
		Subject:      ev.Subject,
		Action:       ActionResourceWrite,
		Outcome:      outcome,
		ResourceType: ev.ResourceType,
		ResourceID:   ev.ResourceID,
		Details:      details,
	})
}

// SyncEvent describes a sync push/pull/conflict audit entry.
type SyncEvent struct {
	Actor        string
	Tenant       string
	Subject      string
	Action       string
	Outcome      string
	ResourceType string
	ResourceID   string
	Details      map[string]string
	Timestamp    time.Time
	ID           string
}

// LogSyncEvent emits a sync-related event. Action should be one of the
// ActionSync* / ActionConflict* / ActionDevice* constants.
func LogSyncEvent(ctx context.Context, logger Logger, ev SyncEvent) error {
	return emit(ctx, logger, Event{
		ID:           ev.ID,
		Timestamp:    ev.Timestamp,
		Actor:        ev.Actor,
		Tenant:       ev.Tenant,
		Subject:      ev.Subject,
		Action:       ev.Action,
		Outcome:      ev.Outcome,
		ResourceType: ev.ResourceType,
		ResourceID:   ev.ResourceID,
		Details:      ev.Details,
	})
}

// AIToolCallEvent describes an AI tool invocation audit entry.
type AIToolCallEvent struct {
	Actor          string
	Tenant         string
	Subject        string
	ToolName       string
	Outcome        string
	ConversationID string
	Details        map[string]string
	Timestamp      time.Time
	ID             string
}

// LogAIToolCall emits an execute-tool event.
func LogAIToolCall(ctx context.Context, logger Logger, ev AIToolCallEvent) error {
	details := cloneDetails(ev.Details)
	if ev.ConversationID != "" {
		details["conversationId"] = ev.ConversationID
	}
	return emit(ctx, logger, Event{
		ID:        ev.ID,
		Timestamp: ev.Timestamp,
		Actor:     ev.Actor,
		Tenant:    ev.Tenant,
		Subject:   ev.Subject,
		Action:    ActionExecuteTool,
		Outcome:   ev.Outcome,
		ToolName:  ev.ToolName,
		Details:   details,
	})
}

// AuthDecisionEvent describes an authorization allow/deny for audit.
type AuthDecisionEvent struct {
	Actor        string
	Tenant       string
	Subject      string
	AuthAction   string // the authorization action checked (read, write, …)
	ResourceType string
	ResourceID   string
	ViewName     string
	ToolName     string
	ModuleName   string
	Allowed      bool
	Reason       string
	Details      map[string]string
	Timestamp    time.Time
	ID           string
}

// LogAuthDecision emits auth.allow or auth.deny.
func LogAuthDecision(ctx context.Context, logger Logger, ev AuthDecisionEvent) error {
	details := cloneDetails(ev.Details)
	if ev.AuthAction != "" {
		details["authAction"] = ev.AuthAction
	}
	if ev.Reason != "" {
		details["reason"] = ev.Reason
	}
	action := ActionAuthDeny
	outcome := OutcomeDeny
	if ev.Allowed {
		action = ActionAuthAllow
		outcome = OutcomeAllow
	}
	return emit(ctx, logger, Event{
		ID:           ev.ID,
		Timestamp:    ev.Timestamp,
		Actor:        ev.Actor,
		Tenant:       ev.Tenant,
		Subject:      ev.Subject,
		Action:       action,
		Outcome:      outcome,
		ResourceType: ev.ResourceType,
		ResourceID:   ev.ResourceID,
		ViewName:     ev.ViewName,
		ToolName:     ev.ToolName,
		ModuleName:   ev.ModuleName,
		Details:      details,
	})
}

// ExportEvent describes an export operation for audit.
type ExportEvent struct {
	Actor     string
	Tenant    string
	Subject   string
	Outcome   string
	ViewName  string
	Details   map[string]string
	Timestamp time.Time
	ID        string
}

// LogExport emits an export event.
func LogExport(ctx context.Context, logger Logger, ev ExportEvent) error {
	outcome := ev.Outcome
	if outcome == "" {
		outcome = OutcomeSuccess
	}
	return emit(ctx, logger, Event{
		ID:        ev.ID,
		Timestamp: ev.Timestamp,
		Actor:     ev.Actor,
		Tenant:    ev.Tenant,
		Subject:   ev.Subject,
		Action:    ActionExport,
		Outcome:   outcome,
		ViewName:  ev.ViewName,
		Details:   ev.Details,
	})
}

// BlobAccessEvent describes blob/binary access for audit.
type BlobAccessEvent struct {
	Actor     string
	Tenant    string
	Subject   string
	BlobKey   string
	Outcome   string
	Details   map[string]string
	Timestamp time.Time
	ID        string
}

// LogBlobAccess emits a blob.access event.
func LogBlobAccess(ctx context.Context, logger Logger, ev BlobAccessEvent) error {
	outcome := ev.Outcome
	if outcome == "" {
		outcome = OutcomeSuccess
	}
	return emit(ctx, logger, Event{
		ID:        ev.ID,
		Timestamp: ev.Timestamp,
		Actor:     ev.Actor,
		Tenant:    ev.Tenant,
		Subject:   ev.Subject,
		Action:    ActionBlobAccess,
		Outcome:   outcome,
		BlobKey:   ev.BlobKey,
		Details:   ev.Details,
	})
}

// ViewAccessEvent describes a view execution for audit.
type ViewAccessEvent struct {
	Actor     string
	Tenant    string
	Subject   string
	ViewName  string
	Version   string
	Outcome   string
	Details   map[string]string
	Timestamp time.Time
	ID        string
}

// LogViewAccess emits an execute-view event.
func LogViewAccess(ctx context.Context, logger Logger, ev ViewAccessEvent) error {
	details := cloneDetails(ev.Details)
	if ev.Version != "" {
		details["viewVersion"] = ev.Version
	}
	return emit(ctx, logger, Event{
		ID:        ev.ID,
		Timestamp: ev.Timestamp,
		Actor:     ev.Actor,
		Tenant:    ev.Tenant,
		Subject:   ev.Subject,
		Action:    ActionExecuteView,
		Outcome:   ev.Outcome,
		ViewName:  ev.ViewName,
		Details:   details,
	})
}
