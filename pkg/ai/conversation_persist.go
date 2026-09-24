package ai

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/degoke/haistack/pkg/store"
)

// LoadConversation loads a persisted session into the harness by conversation id.
func (h *Harness) LoadConversation(ctx context.Context, conversationID string) error {
	if h == nil {
		return errors.New("ai: nil harness")
	}
	if h.cfg.ConversationStore == nil {
		return fmt.Errorf("ai: conversation store not configured")
	}
	conversationID = trimSpace(conversationID)
	if conversationID == "" {
		return ErrInvalidInput
	}
	rec, err := h.cfg.ConversationStore.Get(ctx, conversationID)
	if err != nil {
		return err
	}
	h.session = sessionFromStoreRecord(rec)
	return nil
}

// SaveConversation persists the current in-memory session.
func (h *Harness) SaveConversation(ctx context.Context) error {
	if h == nil {
		return errors.New("ai: nil harness")
	}
	if h.cfg.ConversationStore == nil {
		return fmt.Errorf("ai: conversation store not configured")
	}
	if trimSpace(h.session.ConversationID) == "" {
		return ErrInvalidInput
	}
	rec := storeRecordFromSession(h.cfg.TenantID, h.cfg.Actor, h.cfg.Subject, h.session)
	return h.cfg.ConversationStore.Put(ctx, rec)
}

func (h *Harness) restoreConversationIfStored(ctx context.Context) error {
	if h.cfg.ConversationStore == nil || trimSpace(h.session.ConversationID) == "" {
		return nil
	}
	rec, err := h.cfg.ConversationStore.Get(ctx, h.session.ConversationID)
	if errors.Is(err, store.ErrConversationNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	h.session = sessionFromStoreRecord(rec)
	return nil
}

func (h *Harness) persistConversationIfConfigured(ctx context.Context) error {
	if h.cfg.ConversationStore == nil || h.cfg.DisableConversationPersist {
		return nil
	}
	if trimSpace(h.session.ConversationID) == "" {
		return nil
	}
	return h.SaveConversation(ctx)
}

func sessionFromStoreRecord(rec *store.ConversationRecord) Session {
	if rec == nil {
		return Session{}
	}
	msgs := make([]ChatMessage, 0, len(rec.Messages))
	for _, m := range rec.Messages {
		msg := ChatMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, ChatToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: tc.Arguments,
			})
		}
		msgs = append(msgs, msg)
	}
	return Session{
		ConversationID: rec.ID,
		Messages:       msgs,
	}
}

func storeRecordFromSession(tenantID, actor, subject string, session Session) store.ConversationRecord {
	msgs := make([]store.ConversationMessage, 0, len(session.Messages))
	for _, m := range session.Messages {
		msg := store.ConversationMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, store.ConversationToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: tc.Arguments,
			})
		}
		msgs = append(msgs, msg)
	}
	now := time.Now().UTC()
	return store.ConversationRecord{
		ID:        session.ConversationID,
		TenantID:  tenantID,
		Actor:     actor,
		Subject:   subject,
		Messages:  msgs,
		UpdatedAt: now,
	}
}
