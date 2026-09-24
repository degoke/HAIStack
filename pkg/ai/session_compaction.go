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
	defaultRetainRecentEvents    = 12
	defaultMinEventsToCompact    = 16
	defaultCompactHeadroomTokens = 2048
	compactionSystemPrefix       = "Earlier conversation summary (checkpoint):\n"
)

// SessionCompactionSummarizeInput is passed to a custom compaction summarizer.
type SessionCompactionSummarizeInput struct {
	PriorCheckpointSummary string
	Events                 []store.SessionEvent
}

// SessionCompactionSummarizer compresses a segment of session events into checkpoint text.
type SessionCompactionSummarizer func(ctx context.Context, in SessionCompactionSummarizeInput) (string, error)

// SessionCompactionConfig controls append-only context compaction via checkpoint events.
type SessionCompactionConfig struct {
	// MaxContextTokens triggers compaction when active model context exceeds this estimate (0 = disabled).
	MaxContextTokens int
	// TokenCounter estimates tokens for the active message list (default EstimateChatMessagesTokens).
	TokenCounter ContextTokenCounter
	// Summarizer overrides the default ChatModel summarization (optional).
	Summarizer SessionCompactionSummarizer
	// OnMetric receives compaction telemetry (optional).
	OnMetric func(CompactionMetricEvent)
	// RetainRecentEvents keeps this many newest events after the checkpoint uncompacted (default 12).
	RetainRecentEvents int
	// MinEventsToCompact requires at least this many active events before compacting (default 16).
	MinEventsToCompact int
	// CompactHeadroomTokens compacts when active tokens exceed MaxContextTokens minus this buffer (default 2048).
	CompactHeadroomTokens int
}

func (c SessionCompactionConfig) tokenCounter() ContextTokenCounter {
	if c.TokenCounter != nil {
		return c.TokenCounter
	}
	return EstimateChatMessagesTokens
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

func (c SessionCompactionConfig) compactThreshold() int {
	headroom := c.CompactHeadroomTokens
	if headroom <= 0 {
		headroom = defaultCompactHeadroomTokens
	}
	limit := c.MaxContextTokens - headroom
	if limit < 1 {
		return 1
	}
	return limit
}

// EventsForModelContext splits append-only history into the latest checkpoint summary and active tail events.
func EventsForModelContext(events []store.SessionEvent) (checkpointSummary string, active []store.SessionEvent) {
	summary, active := store.ActiveContextView(events)
	return strings.TrimSpace(summary), active
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
		}
	}
	return out
}

// NewCompactionSessionEvent records a checkpoint summarizing prior active events (append-only).
func NewCompactionSessionEvent(summary string, coveredCount int, lastCoveredEventID, basisTailEventID string, activeTokensEstimate int) store.SessionEvent {
	return store.SessionEvent{
		Author:    store.SessionAuthorCompaction,
		Content:   summary,
		Timestamp: time.Now().UTC(),
		Metadata: map[string]string{
			store.SessionEventMetadataCompaction:            "true",
			store.SessionMetadataCoveredEventCount:          strconv.Itoa(coveredCount),
			store.SessionMetadataLastCoveredEventID:         lastCoveredEventID,
			store.SessionMetadataCompactionBasisTailEventID: basisTailEventID,
			store.SessionMetadataActiveTokensEstimate:       strconv.Itoa(activeTokensEstimate),
		},
	}
}

func (h *Harness) estimateCompactionTokens(messages []ChatMessage, tools []ChatTool) int {
	cfg := h.cfg.SessionCompaction
	counter := cfg.tokenCounter()
	n := counter(messages)
	sp := h.systemPromptForModel(tools)
	if strings.TrimSpace(sp) != "" {
		n += counter([]ChatMessage{{Role: ChatRoleSystem, Content: sp}})
	}
	n += estimateChatToolsTokens(tools, counter)
	return n
}

func (h *Harness) maybeCompactSession(ctx context.Context, tools []ChatTool) error {
	cfg := h.cfg.SessionCompaction
	if cfg.MaxContextTokens <= 0 || h.cfg.SessionService == nil {
		return nil
	}
	threshold := cfg.compactThreshold()
	checkpoint, active := EventsForModelContext(h.persistedEvents)
	messages := chatMessagesFromCheckpoint(checkpoint, active)
	activeTokens := h.estimateCompactionTokens(messages, tools)
	h.recordCompactionMetric(CompactionMetricEvent{
		Kind:             CompactionMetricCheck,
		ActiveTokens:     activeTokens,
		MaxContextTokens: threshold,
	})
	if activeTokens <= threshold {
		h.recordCompactionMetric(CompactionMetricEvent{
			Kind:             CompactionMetricSkippedUnder,
			ActiveTokens:     activeTokens,
			MaxContextTokens: threshold,
		})
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
	basisCheckpointID := ""
	if cp, ok := store.LatestCompactionEvent(h.persistedEvents); ok {
		basisCheckpointID = cp.ID
	}
	basisTailID := active[len(active)-1].ID

	toSummarize := active[:cut]
	summary, err := h.summarizeForCompaction(ctx, checkpoint, toSummarize)
	if err != nil {
		h.recordCompactionMetric(CompactionMetricEvent{
			Kind:             CompactionMetricFailed,
			ActiveTokens:     activeTokens,
			MaxContextTokens: threshold,
			Err:              err,
		})
		return err
	}
	if !h.verifyCompactionBasis(ctx, basisCheckpointID, basisTailID) {
		return nil
	}
	lastID := toSummarize[len(toSummarize)-1].ID
	simulatedActive := append([]store.SessionEvent(nil), active[cut:]...)
	afterMessages := chatMessagesFromCheckpoint(summary, simulatedActive)
	afterTokens := h.estimateCompactionTokens(afterMessages, tools)
	if err := h.appendSessionEvent(ctx, NewCompactionSessionEvent(summary, len(toSummarize), lastID, basisTailID, afterTokens)); err != nil {
		h.recordCompactionMetric(CompactionMetricEvent{
			Kind:             CompactionMetricFailed,
			ActiveTokens:     activeTokens,
			MaxContextTokens: threshold,
			Err:              err,
		})
		return err
	}
	h.refreshMessagesFromPersistedEvents()
	h.recordCompactionMetric(CompactionMetricEvent{
		Kind:               CompactionMetricCompacted,
		ActiveTokens:       activeTokens,
		MaxContextTokens:   threshold,
		EventsSummarized:   len(toSummarize),
		TokensAfterCompact: afterTokens,
	})
	return nil
}

func (h *Harness) verifyCompactionBasis(ctx context.Context, checkpointID, tailID string) bool {
	if h.cfg.SessionService == nil {
		return true
	}
	if err := h.reloadActivePersistedEvents(ctx); err != nil {
		return false
	}
	currentCheckpointID := ""
	if cp, ok := store.LatestCompactionEvent(h.persistedEvents); ok {
		currentCheckpointID = cp.ID
	}
	if currentCheckpointID != checkpointID {
		return false
	}
	_, active := EventsForModelContext(h.persistedEvents)
	if len(active) == 0 {
		return tailID == ""
	}
	return active[len(active)-1].ID == tailID
}

func (h *Harness) reloadActivePersistedEvents(ctx context.Context) error {
	rec, err := h.cfg.SessionService.GetSession(ctx, h.getSessionParams(h.session.ConversationID))
	if err != nil {
		return err
	}
	h.persistedEvents = append([]store.SessionEvent(nil), rec.Events...)
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
	in := SessionCompactionSummarizeInput{
		PriorCheckpointSummary: priorSummary,
		Events:                 events,
	}
	if h.cfg.SessionCompaction.Summarizer != nil {
		return h.cfg.SessionCompaction.Summarizer(ctx, in)
	}
	if h.cfg.Model == nil {
		return "", fmt.Errorf("ai: compaction requires ChatModel or SessionCompaction.Summarizer")
	}
	return h.defaultSummarizeForCompaction(ctx, in)
}

func (h *Harness) defaultSummarizeForCompaction(ctx context.Context, in SessionCompactionSummarizeInput) (string, error) {
	var b strings.Builder
	b.WriteString("Summarize the following conversation segment for use as a durable checkpoint. ")
	b.WriteString("Preserve facts, decisions, patient/resource identifiers, and open tasks. Be concise.\n")
	if strings.TrimSpace(in.PriorCheckpointSummary) != "" {
		b.WriteString("\nPrior checkpoint summary:\n")
		b.WriteString(in.PriorCheckpointSummary)
		b.WriteString("\n")
	}
	b.WriteString("\nSegment to summarize:\n")
	for _, ev := range in.Events {
		if ev.Partial {
			continue
		}
		b.WriteString(fmt.Sprintf("[%s] %s\n", ev.Author, strings.TrimSpace(ev.Content)))
	}
	resp, err := h.cfg.Model.Chat(ctx, ChatRequest{
		Messages:     []ChatMessage{{Role: ChatRoleUser, Content: b.String()}},
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

// ActiveEventsAfterCheckpoint loads tail events after the latest compaction via SessionService.
func (h *Harness) ActiveEventsAfterCheckpoint(ctx context.Context) ([]store.SessionEvent, error) {
	if h.cfg.SessionService == nil || trimSpace(h.session.ConversationID) == "" {
		return nil, nil
	}
	checkpoint, ok := store.LatestCompactionEvent(h.persistedEvents)
	if !ok {
		return append([]store.SessionEvent(nil), h.persistedEvents...), nil
	}
	return h.cfg.SessionService.ListEventsAfter(ctx, store.ListEventsAfterParams{
		TenantID:     h.cfg.TenantID,
		AppName:      h.appName(),
		UserID:       h.userID(),
		SessionID:    h.session.ConversationID,
		AfterEventID: checkpoint.ID,
	})
}
