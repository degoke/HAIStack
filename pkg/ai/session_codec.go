package ai

import (
	"time"

	"github.com/degoke/haistack/pkg/store"
)

// DefaultHarnessAppName is the default SessionService app_name when HarnessConfig.AppName is empty.
const DefaultHarnessAppName = "haistack-harness"

// SessionFromAgentSession maps a stored session into harness memory (events → chat messages).
func SessionFromAgentSession(rec *store.AgentSession) Session {
	if rec == nil {
		return Session{}
	}
	state := rec.State
	if state == nil {
		state = map[string]any{}
	}
	return Session{
		ConversationID: rec.ID,
		Messages:       ChatMessagesFromSessionEvents(rec.Events),
		State:          state,
	}
}

// ChatMessagesFromSessionEvents rebuilds model context from append-only history,
// using the latest compaction checkpoint plus events after it.
func ChatMessagesFromSessionEvents(events []store.SessionEvent) []ChatMessage {
	checkpoint, active := EventsForModelContext(events)
	return chatMessagesFromCheckpoint(checkpoint, active)
}

// NewUserSessionEvent records a user turn.
func NewUserSessionEvent(content string) store.SessionEvent {
	return store.SessionEvent{
		Author:    store.SessionAuthorUser,
		Content:   content,
		Timestamp: time.Now().UTC(),
	}
}

// NewModelSessionEvent records an assistant turn (optional tool calls).
func NewModelSessionEvent(content string, toolCalls []ChatToolCall) store.SessionEvent {
	ev := store.SessionEvent{
		Author:    store.SessionAuthorModel,
		Content:   content,
		Timestamp: time.Now().UTC(),
	}
	for _, tc := range toolCalls {
		ev.ToolCalls = append(ev.ToolCalls, store.SessionToolCall{
			ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments,
		})
	}
	return ev
}

// NewToolSessionEvent records a tool result message.
func NewToolSessionEvent(toolCallID, content string) store.SessionEvent {
	return store.SessionEvent{
		Author:     store.SessionAuthorTool,
		ToolCallID: toolCallID,
		Content:    content,
		Timestamp:  time.Now().UTC(),
	}
}
