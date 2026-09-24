package store

import (
	"context"
	"errors"
	"time"
)

// ErrSessionNotFound indicates the session key does not exist in the tenant scope.
var ErrSessionNotFound = errors.New("session not found")

// ErrSessionEventNotFound indicates the referenced event id is not in the session.
var ErrSessionEventNotFound = errors.New("session event not found")

// SessionEventAuthor identifies who produced an event (ADK-style roles).
const (
	SessionAuthorUser       = "user"
	SessionAuthorModel      = "model"
	SessionAuthorTool       = "tool"
	SessionAuthorSystem     = "system"
	SessionAuthorCompaction = "compaction"
)

// SessionEventMetadataCompaction marks a checkpoint event that summarizes prior history.
const SessionEventMetadataCompaction = "compaction"

// SessionMetadataCoveredEventCount is the number of events summarized into a compaction checkpoint.
const SessionMetadataCoveredEventCount = "coveredEventCount"

// SessionMetadataLastCoveredEventID is the id of the last event included in the compaction summary.
const SessionMetadataLastCoveredEventID = "lastCoveredEventId"

// SessionMetadataActiveTokensEstimate records the estimated active context tokens after compaction.
const SessionMetadataActiveTokensEstimate = "activeTokensEstimate"

// SessionMetadataCompactionBasisTailEventID is the last active tail event id when compaction was planned.
const SessionMetadataCompactionBasisTailEventID = "basisTailEventId"

// SessionToolCall is a persisted tool invocation on a model event.
type SessionToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// SessionEvent is one immutable step in a session history (message, tool, state change).
type SessionEvent struct {
	ID           string            `json:"id"`
	InvocationID string            `json:"invocationId,omitempty"`
	Timestamp    time.Time         `json:"timestamp"`
	Author       string            `json:"author"`
	Partial      bool              `json:"partial,omitempty"`
	Content      string            `json:"content,omitempty"`
	ToolCallID   string            `json:"toolCallId,omitempty"`
	ToolCalls    []SessionToolCall `json:"toolCalls,omitempty"`
	StateDelta   map[string]any    `json:"stateDelta,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// AgentSession is a conversation thread (Google ADK Session analogue).
type AgentSession struct {
	ID        string         `json:"id"`
	TenantID  string         `json:"tenantId"`
	AppName   string         `json:"appName"`
	UserID    string         `json:"userId"`
	Subject   string         `json:"subject,omitempty"`
	State     map[string]any `json:"state"`
	Events    []SessionEvent `json:"events,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// SessionSummary is returned from ListSessions without event payloads.
type SessionSummary struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	AppName   string    `json:"appName"`
	UserID    string    `json:"userId"`
	Subject   string    `json:"subject,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// GetSessionConfig filters events when loading a session (ADK GetSessionConfig).
type GetSessionConfig struct {
	// NumRecentEvents limits how many events are returned (0 = none, <0 invalid).
	NumRecentEvents int
	AfterTimestamp  time.Time
	// ActiveContextOnly returns only the latest compaction checkpoint (if any) and events after it.
	// Use for model context; omit or set false for the full append-only audit log.
	ActiveContextOnly bool
}

// CreateSessionParams creates a new session row.
type CreateSessionParams struct {
	TenantID     string
	AppName      string
	UserID       string
	SessionID    string
	Subject      string
	InitialState map[string]any
}

// GetSessionParams loads a session and optional filtered events.
type GetSessionParams struct {
	TenantID  string
	AppName   string
	UserID    string
	SessionID string
	Config    *GetSessionConfig
}

// ListSessionsParams lists session metadata for an app/user.
type ListSessionsParams struct {
	TenantID string
	AppName  string
	UserID   string
	Limit    int
}

// DeleteSessionParams deletes a session and its events.
type DeleteSessionParams struct {
	TenantID  string
	AppName   string
	UserID    string
	SessionID string
}

// AppendEventParams appends one event and merges StateDelta into session state.
type AppendEventParams struct {
	TenantID  string
	AppName   string
	UserID    string
	SessionID string
	Event     SessionEvent
}

// ListEventsAfterParams returns session events strictly after AfterEventID in append order.
type ListEventsAfterParams struct {
	TenantID     string
	AppName      string
	UserID       string
	SessionID    string
	AfterEventID string
}

// SessionService manages agent sessions and append-only event history (ADK SessionService).
type SessionService interface {
	CreateSession(ctx context.Context, params CreateSessionParams) (*AgentSession, error)
	GetSession(ctx context.Context, params GetSessionParams) (*AgentSession, error)
	ListEventsAfter(ctx context.Context, params ListEventsAfterParams) ([]SessionEvent, error)
	ListSessions(ctx context.Context, params ListSessionsParams) ([]SessionSummary, error)
	DeleteSession(ctx context.Context, params DeleteSessionParams) error
	AppendEvent(ctx context.Context, params AppendEventParams) (*SessionEvent, error)
	GetUserState(ctx context.Context, tenantID, appName, userID string) (map[string]any, error)
	GetAppState(ctx context.Context, tenantID, appName string) (map[string]any, error)
}
