package ai

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/degoke/haistack/pkg/store"
)

const (
	defaultRetainRecentEvents = 12
	defaultMinEventsToCompact = 16
	compactionSystemPrefix    = "Earlier conversation summary (checkpoint):\n"
)

// SessionCompactionConfig controls append-only context compaction via checkpoint events.
type SessionCompactionConfig struct {
	// MaxContextChars triggers compaction when the active model context exceeds this size (0 = disabled).
	MaxContextChars int
	// RetainRecentEvents keeps this many newest events after the checkpoint uncompacted (default 12).
	RetainRecentEvents int
	// MinEventsToCompact requires at least this many active events before compacting (default 16).
	MinEventsToCompact int
}

func (c SessionCompactionConfig) retainRecentEvents() int {
	if c.RetainRecentEvents > 0 {
		return c.RetainRecentEvents
	}
	return defaultRetainRecentEvents
}

func (c SessionCompactionConfig) minEventsToCompact() int {
	if c.MinEventsToCompact > 0 {
		return c.MinEventsToCompact
	}
	return defaultMinEventsToCompact
}

// EventsForModelContext splits append-only history into the latest checkpoint summary and active tail events.
func EventsForModelContext(events []store.SessionEvent) (checkpointSummary string, active []store.SessionEvent) {
	lastIdx := -1
	for i, ev := range events {
		if ev.Partial {
			continue
		}
		if ev.Author == store.SessionAuthorCompaction {
			lastIdx = i
		}
	}
	if lastIdx < 0 {
		return "", append([]store.SessionEvent(nil), events...)
	}
	checkpointSummary = strings.TrimSpace(events[lastIdx].Content)
	active = append([]store.SessionEvent(nil), events[lastIdx+1:]...)
	return checkpointSummary, active
}

func chatMessagesFromCheckpoint(checkpointSummary string, active []store.SessionEvent) []ChatMessage {
	out := make([]ChatMessage, 0, len(active)+1)
	if checkpointSummary != "" {
		out = append(out, ChatMessage{
			Role:    ChatRoleSystem,
			Content: compactionSystemPrefix + checkpointSummary,
		})
	}
	out = append(out, sessionEventsToChatMessages(active)...)
	return out
}

func sessionEventsToChatMessages(events []store.SessionEvent) []ChatMessage {
	out := make([]ChatMessage, 0, len(events))
	for _, ev := range events {
		if ev.Partial {
			continue
		}
		switch ev.Author {
		case store.SessionAuthorUser:
			out = append(out, ChatMessage{Role: ChatRoleUser, Content: ev.Content})
		case store.SessionAuthorModel:
			msg := ChatMessage{Role: ChatRoleAssistant, Content: ev.Content}
			for _, tc := range ev.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, ChatToolCall{
					ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments,
				})
			}
			out = append(out, msg)
		case store.SessionAuthorTool:
			out = append(out, ChatMessage{
				Role: ChatRoleTool, ToolCallID: ev.ToolCallID, Content: ev.Content,
			})
		case store.SessionAuthorSystem:
			out = append(out, ChatMessage{Role: ChatRoleSystem, Content: ev.Content})
		case store.SessionAuthorCompaction:
			// Checkpoints are not replayed as messages; only the latest summary is injected above.
		}
	}
	return out
}

func estimateContextChars(messages []ChatMessage) int {
	n := 0
	for _, m := range messages {
		n += len(m.Content)
		for _, tc := range m.ToolCalls {
			n += len(tc.Name) + len(tc.Arguments) + len(tc.ID)
		}
	}
	return n
}

// NewCompactionSessionEvent records a checkpoint summarizing prior active events (append-only).
func NewCompactionSessionEvent(summary string, coveredCount int, lastCoveredEventID string) store.SessionEvent {
	return store.SessionEvent{
		Author:    store.SessionAuthorCompaction,
		Content:   summary,
		Timestamp: storeEventTime(),
		Metadata: map[string]string{
			store.SessionEventMetadataCompaction:       "true",
			store.SessionMetadataCoveredEventCount:     strconv.Itoa(coveredCount),
			store.SessionMetadataLastCoveredEventID:    lastCoveredEventID,
		},
	}
}

func storeEventTime() time.Time {
	return time.Now().UTC()
}

func (h *Harness) maybeCompactSession(ctx context.Context) error {
	cfg := h.cfg.SessionCompaction
	if cfg.MaxContextChars <= 0 || h.cfg.SessionService == nil || h.cfg.Model == nil {
		return nil
	}
	checkpoint, active := EventsForModelContext(h.persistedEvents)
	messages := chatMessagesFromCheckpoint(checkpoint, active)
	if estimateContextChars(messages) <= cfg.MaxContextChars {
		return nil
	}
	if len(active) < cfg.minEventsToCompact() {
		return nil
	}
	retain := cfg.retainRecentEvents()
	if len(active) <= retain {
		return nil
	}
	cut := len(active) - retain
	cut = alignCompactionCutToUserBoundary(active, cut)
	if cut <= 0 {
		return nil
	}
	toSummarize := active[:cut]
	summary, err := h.summarizeForCompaction(ctx, checkpoint, toSummarize)
	if err != nil {
		return err
	}
	lastID := toSummarize[len(toSummarize)-1].ID
	if err := h.appendSessionEvent(ctx, NewCompactionSessionEvent(summary, len(toSummarize), lastID)); err != nil {
		return err
	}
	h.refreshMessagesFromPersistedEvents()
	return nil
}

// alignCompactionCutToUserBoundary moves the cut index backward to a user event so tool rounds stay in the tail.
func alignCompactionCutToUserBoundary(active []store.SessionEvent, cut int) int {
	for cut > 0 && active[cut].Author != store.SessionAuthorUser {
		cut--
	}
	return cut
}

func (h *Harness) summarizeForCompaction(ctx context.Context, priorSummary string, events []store.SessionEvent) (string, error) {
	var b strings.Builder
	b.WriteString("Summarize the following conversation segment for use as a durable checkpoint. ")
	b.WriteString("Preserve facts, decisions, patient/resource identifiers, and open tasks. Be concise.\n")
	if strings.TrimSpace(priorSummary) != "" {
		b.WriteString("\nPrior checkpoint summary:\n")
		b.WriteString(priorSummary)
		b.WriteString("\n")
	}
	b.WriteString("\nSegment to summarize:\n")
	for _, ev := range events {
		if ev.Partial {
			continue
		}
		b.WriteString(fmt.Sprintf("[%s] %s\n", ev.Author, strings.TrimSpace(ev.Content)))
	}
	resp, err := h.cfg.Model.Chat(ctx, ChatRequest{
		Messages: []ChatMessage{{Role: ChatRoleUser, Content: b.String()}},
		SystemPrompt: "You produce factual conversation checkpoints for a clinical agent. Output only the summary text.",
		Hint:         h.cfg.ModelHint,
	})
	if err != nil {
		return "", err
	}
	summary := strings.TrimSpace(resp.Content)
	if summary == "" {
		return "", fmt.Errorf("ai: compaction summarizer returned empty summary")
	}
	return summary, nil
}

func (h *Harness) refreshMessagesFromPersistedEvents() {
	checkpoint, active := EventsForModelContext(h.persistedEvents)
	h.session.Messages = chatMessagesFromCheckpoint(checkpoint, active)
}
