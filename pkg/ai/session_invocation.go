package ai

import (
	"strings"

	"github.com/degoke/haistack/pkg/store"
)

func invocationEvents(events []store.SessionEvent, invocationID string) []store.SessionEvent {
	if invocationID == "" {
		return nil
	}
	out := make([]store.SessionEvent, 0, len(events))
	for _, ev := range events {
		if ev.InvocationID == invocationID {
			out = append(out, ev)
		}
	}
	return out
}

func hasUserEventForInvocation(events []store.SessionEvent, invocationID string) bool {
	for _, ev := range events {
		if ev.InvocationID == invocationID && ev.Author == store.SessionAuthorUser {
			return true
		}
	}
	return false
}

// invocationTerminalAnswer reports whether an invocation finished with a final model message (no tool calls).
func invocationTerminalAnswer(events []store.SessionEvent, invocationID string) (string, bool) {
	inv := invocationEvents(events, invocationID)
	if len(inv) == 0 {
		return "", false
	}
	last := inv[len(inv)-1]
	if last.Author != store.SessionAuthorModel || len(last.ToolCalls) > 0 {
		return "", false
	}
	return strings.TrimSpace(last.Content), true
}

func tagInvocation(event store.SessionEvent, invocationID string) store.SessionEvent {
	if invocationID != "" && strings.TrimSpace(event.InvocationID) == "" {
		event.InvocationID = invocationID
	}
	return event
}
