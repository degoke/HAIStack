package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/haistack/pkg/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionService persists ADK-style agent sessions for one tenant in Postgres.
type SessionService struct {
	exec     querier
	tenantID string
}

func newSessionService(pool *pgxpool.Pool, tenantID string) *SessionService {
	return &SessionService{exec: pool, tenantID: tenantID}
}

// CreateSession implements store.SessionService.
func (s *SessionService) CreateSession(ctx context.Context, params store.CreateSessionParams) (*store.AgentSession, error) {
	tenant := s.tenantID
	if params.TenantID != "" {
		tenant = params.TenantID
	}
	sessionID := strings.TrimSpace(params.SessionID)
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	now := time.Now().UTC()
	state := params.InitialState
	if state == nil {
		state = map[string]any{}
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	_, err = s.exec.Exec(ctx, `
		INSERT INTO hai_agent_session (
			tenant_id, app_name, user_id, id, subject, state_json, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		tenant, params.AppName, params.UserID, sessionID,
		nullString(params.Subject), stateJSON, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &store.AgentSession{
		ID: sessionID, TenantID: tenant, AppName: params.AppName, UserID: params.UserID,
		Subject: params.Subject, State: state, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// ListEventsAfter implements store.SessionService.
func (s *SessionService) ListEventsAfter(ctx context.Context, params store.ListEventsAfterParams) ([]store.SessionEvent, error) {
	tenant := s.tenantID
	if params.TenantID != "" {
		tenant = params.TenantID
	}
	afterID := strings.TrimSpace(params.AfterEventID)
	if afterID == "" {
		return nil, fmt.Errorf("list events after: missing after event id")
	}
	rows, err := s.exec.Query(ctx, `
		SELECT e.event_json FROM hai_agent_session_event e
		WHERE e.tenant_id = $1 AND e.app_name = $2 AND e.user_id = $3 AND e.session_id = $4
		  AND (e.timestamp, e.event_id) > (
		    SELECT c.timestamp, c.event_id FROM hai_agent_session_event c
		    WHERE c.tenant_id = $1 AND c.app_name = $2 AND c.user_id = $3 AND c.session_id = $4 AND c.event_id = $5
		  )
		ORDER BY e.timestamp ASC, e.event_id ASC`,
		tenant, params.AppName, params.UserID, params.SessionID, afterID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []store.SessionEvent
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var ev store.SessionEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(events) == 0 {
		var found int
		err = s.exec.QueryRow(ctx, `
			SELECT 1 FROM hai_agent_session_event
			WHERE tenant_id = $1 AND app_name = $2 AND user_id = $3 AND session_id = $4 AND event_id = $5`,
			tenant, params.AppName, params.UserID, params.SessionID, afterID,
		).Scan(&found)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, store.ErrSessionEventNotFound
			}
			return nil, err
		}
	}
	return events, nil
}

// GetSession implements store.SessionService.
func (s *SessionService) GetSession(ctx context.Context, params store.GetSessionParams) (*store.AgentSession, error) {
	tenant := s.tenantID
	if params.TenantID != "" {
		tenant = params.TenantID
	}
	row := s.exec.QueryRow(ctx, `
		SELECT id, tenant_id, app_name, user_id, subject, state_json, created_at, updated_at
		FROM hai_agent_session
		WHERE tenant_id = $1 AND app_name = $2 AND user_id = $3 AND id = $4`,
		tenant, params.AppName, params.UserID, params.SessionID,
	)
	session, err := scanAgentSessionRow(row)
	if err != nil {
		return nil, err
	}
	events, err := s.listEvents(ctx, tenant, params.AppName, params.UserID, params.SessionID, params.Config)
	if err != nil {
		return nil, err
	}
	session.Events = events
	return session, nil
}

// ListSessions implements store.SessionService.
func (s *SessionService) ListSessions(ctx context.Context, params store.ListSessionsParams) ([]store.SessionSummary, error) {
	tenant := s.tenantID
	if params.TenantID != "" {
		tenant = params.TenantID
	}
	query := `
		SELECT id, tenant_id, app_name, user_id, subject, updated_at
		FROM hai_agent_session
		WHERE tenant_id = $1 AND app_name = $2`
	args := []any{tenant, params.AppName}
	argN := 3
	if params.UserID != "" {
		query += fmt.Sprintf(" AND user_id = $%d", argN)
		args = append(args, params.UserID)
		argN++
	}
	query += " ORDER BY updated_at ASC"
	if params.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", params.Limit)
	}
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	var out []store.SessionSummary
	for rows.Next() {
		var (
			id, tenantID, app, user string
			subject                 *string
			updated                 time.Time
		)
		if err := rows.Scan(&id, &tenantID, &app, &user, &subject, &updated); err != nil {
			return nil, err
		}
		sum := store.SessionSummary{ID: id, TenantID: tenantID, AppName: app, UserID: user, UpdatedAt: updated}
		if subject != nil {
			sum.Subject = *subject
		}
		out = append(out, sum)
	}
	return out, rows.Err()
}

// DeleteSession implements store.SessionService.
func (s *SessionService) DeleteSession(ctx context.Context, params store.DeleteSessionParams) error {
	tenant := s.tenantID
	if params.TenantID != "" {
		tenant = params.TenantID
	}
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM hai_agent_session
		WHERE tenant_id = $1 AND app_name = $2 AND user_id = $3 AND id = $4`,
		tenant, params.AppName, params.UserID, params.SessionID,
	)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrSessionNotFound
	}
	return nil
}

// AppendEvent implements store.SessionService.
func (s *SessionService) AppendEvent(ctx context.Context, params store.AppendEventParams) (*store.SessionEvent, error) {
	if params.Event.Partial {
		return &params.Event, nil
	}
	tenant := s.tenantID
	if params.TenantID != "" {
		tenant = params.TenantID
	}
	event := params.Event
	if strings.TrimSpace(event.ID) == "" {
		event.ID = uuid.NewString()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	_, err = s.exec.Exec(ctx, `
		INSERT INTO hai_agent_session_event (
			tenant_id, app_name, user_id, session_id, event_id, timestamp, event_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		tenant, params.AppName, params.UserID, params.SessionID,
		event.ID, event.Timestamp, eventJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}
	if err := s.applyStateDeltas(ctx, tenant, params.AppName, params.UserID, params.SessionID, event.StateDelta); err != nil {
		return nil, err
	}
	_, err = s.exec.Exec(ctx, `
		UPDATE hai_agent_session SET updated_at = $1
		WHERE tenant_id = $2 AND app_name = $3 AND user_id = $4 AND id = $5`,
		event.Timestamp, tenant, params.AppName, params.UserID, params.SessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("touch session: %w", err)
	}
	return &event, nil
}

// GetUserState implements store.SessionService.
func (s *SessionService) GetUserState(ctx context.Context, tenantID, appName, userID string) (map[string]any, error) {
	tenant := s.tenantID
	if tenantID != "" {
		tenant = tenantID
	}
	row := s.exec.QueryRow(ctx, `
		SELECT state_json FROM hai_agent_user_state
		WHERE tenant_id = $1 AND app_name = $2 AND user_id = $3`, tenant, appName, userID)
	return scanStateJSONRow(row)
}

// GetAppState implements store.SessionService.
func (s *SessionService) GetAppState(ctx context.Context, tenantID, appName string) (map[string]any, error) {
	tenant := s.tenantID
	if tenantID != "" {
		tenant = tenantID
	}
	row := s.exec.QueryRow(ctx, `
		SELECT state_json FROM hai_agent_app_state
		WHERE tenant_id = $1 AND app_name = $2`, tenant, appName)
	return scanStateJSONRow(row)
}

func (s *SessionService) listEvents(ctx context.Context, tenant, appName, userID, sessionID string, cfg *store.GetSessionConfig) ([]store.SessionEvent, error) {
	query := `
		SELECT event_json FROM hai_agent_session_event
		WHERE tenant_id = $1 AND app_name = $2 AND user_id = $3 AND session_id = $4`
	args := []any{tenant, appName, userID, sessionID}
	argN := 5
	if cfg != nil && !cfg.AfterTimestamp.IsZero() {
		query += fmt.Sprintf(" AND timestamp >= $%d", argN)
		args = append(args, cfg.AfterTimestamp)
		argN++
	}
	query += " ORDER BY timestamp ASC"
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []store.SessionEvent
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var ev store.SessionEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if cfg != nil {
		if cfg.NumRecentEvents == 0 {
			return nil, nil
		}
		if cfg.NumRecentEvents > 0 && len(events) > cfg.NumRecentEvents {
			events = events[len(events)-cfg.NumRecentEvents:]
		}
		if cfg.ActiveContextOnly {
			events = store.ActiveContextEvents(events)
		}
	}
	return events, nil
}

func (s *SessionService) applyStateDeltas(ctx context.Context, tenant, appName, userID, sessionID string, delta map[string]any) error {
	if len(delta) == 0 {
		return nil
	}
	sessionDelta := map[string]any{}
	userDelta := map[string]any{}
	appDelta := map[string]any{}
	for key, value := range delta {
		switch {
		case strings.HasPrefix(key, "app:"):
			appDelta[strings.TrimPrefix(key, "app:")] = value
		case strings.HasPrefix(key, "user:"):
			userDelta[strings.TrimPrefix(key, "user:")] = value
		default:
			sessionDelta[key] = value
		}
	}
	if len(sessionDelta) > 0 {
		if err := s.mergeSessionState(ctx, tenant, appName, userID, sessionID, sessionDelta); err != nil {
			return err
		}
	}
	if len(userDelta) > 0 {
		if err := s.mergeUserState(ctx, tenant, appName, userID, userDelta); err != nil {
			return err
		}
	}
	if len(appDelta) > 0 {
		if err := s.mergeAppState(ctx, tenant, appName, appDelta); err != nil {
			return err
		}
	}
	return nil
}

func (s *SessionService) mergeSessionState(ctx context.Context, tenant, appName, userID, sessionID string, delta map[string]any) error {
	state, err := scanStateJSONRow(s.exec.QueryRow(ctx, `
		SELECT state_json FROM hai_agent_session
		WHERE tenant_id = $1 AND app_name = $2 AND user_id = $3 AND id = $4`,
		tenant, appName, userID, sessionID))
	if err != nil {
		return err
	}
	for k, v := range delta {
		state[k] = v
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = s.exec.Exec(ctx, `
		UPDATE hai_agent_session SET state_json = $1
		WHERE tenant_id = $2 AND app_name = $3 AND user_id = $4 AND id = $5`,
		raw, tenant, appName, userID, sessionID)
	return err
}

func (s *SessionService) mergeUserState(ctx context.Context, tenant, appName, userID string, delta map[string]any) error {
	state, _ := scanStateJSONRow(s.exec.QueryRow(ctx, `
		SELECT state_json FROM hai_agent_user_state
		WHERE tenant_id = $1 AND app_name = $2 AND user_id = $3`, tenant, appName, userID))
	for k, v := range delta {
		state[k] = v
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = s.exec.Exec(ctx, `
		INSERT INTO hai_agent_user_state (tenant_id, app_name, user_id, state_json, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, app_name, user_id) DO UPDATE SET
			state_json = EXCLUDED.state_json,
			updated_at = EXCLUDED.updated_at`,
		tenant, appName, userID, raw, now)
	return err
}

func (s *SessionService) mergeAppState(ctx context.Context, tenant, appName string, delta map[string]any) error {
	state, _ := scanStateJSONRow(s.exec.QueryRow(ctx, `
		SELECT state_json FROM hai_agent_app_state
		WHERE tenant_id = $1 AND app_name = $2`, tenant, appName))
	for k, v := range delta {
		state[k] = v
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = s.exec.Exec(ctx, `
		INSERT INTO hai_agent_app_state (tenant_id, app_name, state_json, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, app_name) DO UPDATE SET
			state_json = EXCLUDED.state_json,
			updated_at = EXCLUDED.updated_at`,
		tenant, appName, raw, now)
	return err
}

func scanAgentSessionRow(row pgx.Row) (*store.AgentSession, error) {
	var (
		id, tenant, app, user string
		subject               *string
		stateJSON             []byte
		created, updated      time.Time
	)
	if err := row.Scan(&id, &tenant, &app, &user, &subject, &stateJSON, &created, &updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrSessionNotFound
		}
		return nil, fmt.Errorf("scan session: %w", err)
	}
	state := map[string]any{}
	if len(stateJSON) > 0 {
		_ = json.Unmarshal(stateJSON, &state)
	}
	sess := &store.AgentSession{
		ID: id, TenantID: tenant, AppName: app, UserID: user,
		State: state, CreatedAt: created, UpdatedAt: updated,
	}
	if subject != nil {
		sess.Subject = *subject
	}
	return sess, nil
}

func scanStateJSONRow(row pgx.Row) (map[string]any, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	state := map[string]any{}
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	return state, nil
}
