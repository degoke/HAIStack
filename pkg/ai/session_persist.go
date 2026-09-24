package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/degoke/haistack/pkg/store"
)

// LoadAgentSession loads session metadata, state, and events into the harness.
func (h *Harness) LoadAgentSession(ctx context.Context, sessionID string) error {
	if h == nil {
		return errors.New("ai: nil harness")
	}
	if h.cfg.SessionService == nil {
		return fmt.Errorf("ai: session service not configured")
	}
	sessionID = trimSpace(sessionID)
	if sessionID == "" {
		return ErrInvalidInput
	}
	rec, err := h.cfg.SessionService.GetSession(ctx, h.getSessionParams(sessionID))
	if err != nil {
		return err
	}
	h.session = SessionFromAgentSession(rec)
	return nil
}

// CreateAgentSession creates a new persisted session and binds it to the harness.
func (h *Harness) CreateAgentSession(ctx context.Context, sessionID string) error {
	if h == nil {
		return errors.New("ai: nil harness")
	}
	if h.cfg.SessionService == nil {
		return fmt.Errorf("ai: session service not configured")
	}
	return h.createAgentSessionPreserveMemory(ctx, sessionID)
}

func (h *Harness) appName() string {
	if strings.TrimSpace(h.cfg.AppName) != "" {
		return strings.TrimSpace(h.cfg.AppName)
	}
	return DefaultHarnessAppName
}

func (h *Harness) userID() string {
	if strings.TrimSpace(h.cfg.Actor) == "" {
		return "anonymous"
	}
	return strings.TrimSpace(h.cfg.Actor)
}

func (h *Harness) getSessionConfig() *store.GetSessionConfig {
	if h.cfg.SessionEventLimit <= 0 {
		return nil
	}
	return &store.GetSessionConfig{NumRecentEvents: h.cfg.SessionEventLimit}
}

func (h *Harness) getSessionParams(sessionID string) store.GetSessionParams {
	return store.GetSessionParams{
		TenantID:  h.cfg.TenantID,
		AppName:   h.appName(),
		UserID:    h.userID(),
		SessionID: sessionID,
		Config:    h.getSessionConfig(),
	}
}

func (h *Harness) restoreAgentSessionIfStored(ctx context.Context) error {
	if h.cfg.SessionService == nil || trimSpace(h.session.ConversationID) == "" {
		return nil
	}
	sessionID := h.session.ConversationID
	rec, err := h.cfg.SessionService.GetSession(ctx, h.getSessionParams(sessionID))
	if errors.Is(err, store.ErrSessionNotFound) {
		return h.createAgentSessionPreserveMemory(ctx, sessionID)
	}
	if err != nil {
		return err
	}
	if len(h.session.Messages) == 0 {
		h.session = SessionFromAgentSession(rec)
		return nil
	}
	h.mergeStoredSessionState(rec.State)
	return nil
}

func (h *Harness) createAgentSessionPreserveMemory(ctx context.Context, sessionID string) error {
	preserve := h.session
	rec, err := h.cfg.SessionService.CreateSession(ctx, store.CreateSessionParams{
		TenantID:     h.cfg.TenantID,
		AppName:      h.appName(),
		UserID:       h.userID(),
		SessionID:    sessionID,
		Subject:      h.cfg.Subject,
		InitialState: preserve.State,
	})
	if err != nil {
		return err
	}
	h.session = SessionFromAgentSession(rec)
	h.session.ConversationID = sessionID
	if len(preserve.Messages) > 0 {
		h.session.Messages = preserve.Messages
	}
	if len(preserve.State) > 0 {
		h.mergeStoredSessionState(preserve.State)
	}
	return nil
}

func (h *Harness) mergeStoredSessionState(stored map[string]any) {
	if len(stored) == 0 {
		return
	}
	if h.session.State == nil {
		h.session.State = map[string]any{}
	}
	for k, v := range stored {
		h.session.State[k] = v
	}
}

func (h *Harness) appendSessionEvent(ctx context.Context, event store.SessionEvent) error {
	if h.cfg.SessionService == nil || trimSpace(h.session.ConversationID) == "" {
		return nil
	}
	_, err := h.cfg.SessionService.AppendEvent(ctx, store.AppendEventParams{
		TenantID:  h.cfg.TenantID,
		AppName:   h.appName(),
		UserID:    h.userID(),
		SessionID: h.session.ConversationID,
		Event:     event,
	})
	if err != nil {
		return err
	}
	h.applySessionStateDelta(event.StateDelta)
	return nil
}

func (h *Harness) applySessionStateDelta(delta map[string]any) {
	if len(delta) == 0 {
		return
	}
	if h.session.State == nil {
		h.session.State = map[string]any{}
	}
	for key, value := range delta {
		if strings.HasPrefix(key, "app:") || strings.HasPrefix(key, "user:") {
			continue
		}
		h.session.State[key] = value
	}
}
