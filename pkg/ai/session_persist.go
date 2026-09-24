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
	rec, err := h.cfg.SessionService.GetSession(ctx, store.GetSessionParams{
		TenantID:  h.cfg.TenantID,
		AppName:   h.appName(),
		UserID:    h.userID(),
		SessionID: sessionID,
	})
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
	rec, err := h.cfg.SessionService.CreateSession(ctx, store.CreateSessionParams{
		TenantID:     h.cfg.TenantID,
		AppName:      h.appName(),
		UserID:       h.userID(),
		SessionID:    sessionID,
		Subject:      h.cfg.Subject,
		InitialState: h.session.State,
	})
	if err != nil {
		return err
	}
	h.session = SessionFromAgentSession(rec)
	return nil
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

func (h *Harness) restoreAgentSessionIfStored(ctx context.Context) error {
	if h.cfg.SessionService == nil || trimSpace(h.session.ConversationID) == "" {
		return nil
	}
	rec, err := h.cfg.SessionService.GetSession(ctx, store.GetSessionParams{
		TenantID:  h.cfg.TenantID,
		AppName:   h.appName(),
		UserID:    h.userID(),
		SessionID: h.session.ConversationID,
	})
	if errors.Is(err, store.ErrSessionNotFound) {
		return h.CreateAgentSession(ctx, h.session.ConversationID)
	}
	if err != nil {
		return err
	}
	h.session = SessionFromAgentSession(rec)
	return nil
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
	return err
}
