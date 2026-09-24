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
	h.bindAgentSession(rec, sessionID)
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
	return h.createAgentSession(ctx, trimSpace(sessionID))
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

func (h *Harness) getSessionParams(sessionID string) store.GetSessionParams {
	return store.GetSessionParams{
		TenantID:  h.cfg.TenantID,
		AppName:   h.appName(),
		UserID:    h.userID(),
		SessionID: sessionID,
	}
}

// ensureSessionLoaded binds conversation id and reloads session events and state from SessionService.
func (h *Harness) ensureSessionLoaded(ctx context.Context) error {
	if h == nil {
		return errors.New("ai: nil harness")
	}
	h.ensureConversationID()
	return h.restoreAgentSessionIfStored(ctx)
}

// restoreAgentSessionIfStored reloads transcript and state from SessionService before each Chat.
// When SessionService is configured, the store is the source of truth (not preloaded Session.Messages).
func (h *Harness) restoreAgentSessionIfStored(ctx context.Context) error {
	if h.cfg.SessionService == nil || trimSpace(h.session.ConversationID) == "" {
		return nil
	}
	sessionID := h.session.ConversationID
	rec, err := h.cfg.SessionService.GetSession(ctx, h.getSessionParams(sessionID))
	if errors.Is(err, store.ErrSessionNotFound) {
		return h.createAgentSession(ctx, sessionID)
	}
	if err != nil {
		return err
	}
	h.bindAgentSession(rec, sessionID)
	return nil
}

func (h *Harness) bindAgentSession(rec *store.AgentSession, sessionID string) {
	h.session = SessionFromAgentSession(rec)
	h.session.ConversationID = sessionID
	if rec != nil {
		h.persistedEvents = append([]store.SessionEvent(nil), rec.Events...)
	} else {
		h.persistedEvents = nil
	}
}

func (h *Harness) createAgentSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return ErrInvalidInput
	}
	rec, err := h.cfg.SessionService.CreateSession(ctx, store.CreateSessionParams{
		TenantID:     h.cfg.TenantID,
		AppName:      h.appName(),
		UserID:       h.userID(),
		SessionID:    sessionID,
		Subject:      h.cfg.Subject,
		InitialState: map[string]any{},
	})
	if err != nil {
		return err
	}
	h.bindAgentSession(rec, sessionID)
	return nil
}

func (h *Harness) appendSessionEvent(ctx context.Context, event store.SessionEvent) error {
	event = tagInvocation(event, h.activeInvocationID)
	if h.cfg.SessionService == nil || trimSpace(h.session.ConversationID) == "" {
		if h.activeInvocationID != "" {
			h.persistedEvents = append(h.persistedEvents, event)
		}
		return nil
	}
	stored, err := h.cfg.SessionService.AppendEvent(ctx, store.AppendEventParams{
		TenantID:  h.cfg.TenantID,
		AppName:   h.appName(),
		UserID:    h.userID(),
		SessionID: h.session.ConversationID,
		Event:     event,
	})
	if err != nil {
		return err
	}
	if stored != nil {
		h.persistedEvents = append(h.persistedEvents, *stored)
	} else {
		h.persistedEvents = append(h.persistedEvents, event)
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
