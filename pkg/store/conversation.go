package store

import (
	"context"
	"errors"
	"time"
)

// ErrConversationNotFound indicates a conversation id does not exist for the tenant scope.
var ErrConversationNotFound = errors.New("conversation not found")

// ConversationToolCall is a persisted model tool invocation.
type ConversationToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ConversationMessage is one persisted chat turn.
type ConversationMessage struct {
	Role       string                 `json:"role"`
	Content    string                 `json:"content,omitempty"`
	ToolCallID string                 `json:"toolCallId,omitempty"`
	ToolCalls  []ConversationToolCall `json:"toolCalls,omitempty"`
}

// ConversationRecord is one agent chat session row.
type ConversationRecord struct {
	ID        string                `json:"id"`
	TenantID  string                `json:"tenantId"`
	Actor     string                `json:"actor,omitempty"`
	Subject   string                `json:"subject,omitempty"`
	Messages  []ConversationMessage `json:"messages"`
	CreatedAt time.Time             `json:"createdAt"`
	UpdatedAt time.Time             `json:"updatedAt"`
}

// ConversationQuery lists conversations for a tenant scope.
type ConversationQuery struct {
	Actor  string `json:"actor,omitempty"`
	Subject string `json:"subject,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// ConversationStore persists agent chat sessions (harness layer).
type ConversationStore interface {
	Get(ctx context.Context, id string) (*ConversationRecord, error)
	Put(ctx context.Context, record ConversationRecord) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, query ConversationQuery) ([]ConversationRecord, error)
}
