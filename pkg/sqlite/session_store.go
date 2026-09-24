package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/haistack/pkg/store"
	"github.com/google/uuid"
)

// SessionService persists ADK-style agent sessions for one tenant in SQLite.
type SessionService struct {
	exec     moduleExec
	tenantID string
}

func newSessionService(db *sql.DB, tenantID string) *SessionService {
	return &SessionService{exec: db, tenantID: tenantID}
}

var _ store.SessionService = (*SessionService)(nil)

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
	_, err = s.exec.ExecContext(ctx, `
		INSERT INTO hai_agent_session (
			tenant_id, app_name, user_id, id, subject, state_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		tenant, params.AppName, params.UserID, sessionID,
		nullString(params.Subject), string(stateJSON), formatTime(now), formatTime(now),
	)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &store.AgentSession{
		ID:        sessionID,
		TenantID:  tenant,
		AppName:   params.AppName,
		UserID:    params.UserID,
		Subject:   params.Subject,
		State:     state,
		Events:    nil,
		CreatedAt: now,
		UpdatedAt: now,
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
	rows, err := s.exec.QueryContext(ctx, `
		SELECT event_json FROM hai_agent_session_event
		WHERE tenant_id = ? AND app_name = ? AND user_id = ? AND session_id = ?
		  AND rowid > (
		    SELECT rowid FROM hai_agent_session_event
		    WHERE tenant_id = ? AND app_name = ? AND user_id = ? AND session_id = ? AND event_id = ?
		  )
		ORDER BY rowid ASC`,
		tenant, params.AppName, params.UserID, params.SessionID,
		tenant, params.AppName, params.UserID, params.SessionID, afterID,
	)
	if err != nil {
		return nil, fmt.Errorf("list events after: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var events []store.SessionEvent
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var ev store.SessionEvent
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(events) == 0 {
		var found int
		err = s.exec.QueryRowContext(ctx, `
			SELECT 1 FROM hai_agent_session_event
			WHERE tenant_id = ? AND app_name = ? AND user_id = ? AND session_id = ? AND event_id = ?`,
			tenant, params.AppName, params.UserID, params.SessionID, afterID,
		).Scan(&found)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
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
	row := s.exec.QueryRowContext(ctx, `
		SELECT id, tenant_id, app_name, user_id, subject, state_json, created_at, updated_at
		FROM hai_agent_session
		WHERE tenant_id = ? AND app_name = ? AND user_id = ? AND id = ?`,
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
		WHERE tenant_id = ? AND app_name = ?`
	args := []any{tenant, params.AppName}
	if params.UserID != "" {
		query += " AND user_id = ?"
		args = append(args, params.UserID)
	}
	query += " ORDER BY updated_at ASC"
	if params.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", params.Limit)
	}
	rows, err := s.exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.SessionSummary
	for rows.Next() {
		var (
			id, tenantID, app, user string
			subject                 sql.NullString
			updated                 string
		)
		if err := rows.Scan(&id, &tenantID, &app, &user, &subject, &updated); err != nil {
			return nil, err
		}
		updatedAt, err := parseTime(updated)
		if err != nil {
			return nil, err
		}
		sum := store.SessionSummary{
			ID: id, TenantID: tenantID, AppName: app, UserID: user, UpdatedAt: updatedAt,
		}
		if subject.Valid {
			sum.Subject = subject.String
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
	res, err := s.exec.ExecContext(ctx, `
		DELETE FROM hai_agent_session
		WHERE tenant_id = ? AND app_name = ? AND user_id = ? AND id = ?`,
		tenant, params.AppName, params.UserID, params.SessionID,
	)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
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
	_, err = s.exec.ExecContext(ctx, `
		INSERT INTO hai_agent_session_event (
			tenant_id, app_name, user_id, session_id, event_id, timestamp, event_json
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tenant, params.AppName, params.UserID, params.SessionID,
		event.ID, formatTime(event.Timestamp), string(eventJSON),
	)
	if err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}
	if err := s.applyStateDeltas(ctx, tenant, params.AppName, params.UserID, params.SessionID, event.StateDelta); err != nil {
		return nil, err
	}
	_, err = s.exec.ExecContext(ctx, `
		UPDATE hai_agent_session SET updated_at = ? WHERE tenant_id = ? AND app_name = ? AND user_id = ? AND id = ?`,
		formatTime(event.Timestamp), tenant, params.AppName, params.UserID, params.SessionID,
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
	row := s.exec.QueryRowContext(ctx, `
		SELECT state_json FROM hai_agent_user_state
		WHERE tenant_id = ? AND app_name = ? AND user_id = ?`, tenant, appName, userID)
	return scanStateJSON(row)
}

// GetAppState implements store.SessionService.
func (s *SessionService) GetAppState(ctx context.Context, tenantID, appName string) (map[string]any, error) {
	tenant := s.tenantID
	if tenantID != "" {
		tenant = tenantID
	}
	row := s.exec.QueryRowContext(ctx, `
		SELECT state_json FROM hai_agent_app_state
		WHERE tenant_id = ? AND app_name = ?`, tenant, appName)
	return scanStateJSON(row)
}

func (s *SessionService) listEvents(ctx context.Context, tenant, appName, userID, sessionID string, cfg *store.GetSessionConfig) ([]store.SessionEvent, error) {
	query := `
		SELECT event_json FROM hai_agent_session_event
		WHERE tenant_id = ? AND app_name = ? AND user_id = ? AND session_id = ?`
	args := []any{tenant, appName, userID, sessionID}
	if cfg != nil && !cfg.AfterTimestamp.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, formatTime(cfg.AfterTimestamp))
	}
	query += " ORDER BY timestamp ASC"
	rows, err := s.exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var events []store.SessionEvent
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var ev store.SessionEvent
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
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
		if err := s.mergeScopedState(ctx, "hai_agent_user_state",
			"tenant_id = ? AND app_name = ? AND user_id = ?", []any{tenant, appName, userID}, userDelta); err != nil {
			return err
		}
	}
	if len(appDelta) > 0 {
		if err := s.mergeScopedState(ctx, "hai_agent_app_state",
			"tenant_id = ? AND app_name = ?", []any{tenant, appName}, appDelta); err != nil {
			return err
		}
	}
	return nil
}

func (s *SessionService) mergeSessionState(ctx context.Context, tenant, appName, userID, sessionID string, delta map[string]any) error {
	row := s.exec.QueryRowContext(ctx, `
		SELECT state_json FROM hai_agent_session
		WHERE tenant_id = ? AND app_name = ? AND user_id = ? AND id = ?`,
		tenant, appName, userID, sessionID)
	state, err := scanStateJSON(row)
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
	_, err = s.exec.ExecContext(ctx, `
		UPDATE hai_agent_session SET state_json = ? WHERE tenant_id = ? AND app_name = ? AND user_id = ? AND id = ?`,
		string(raw), tenant, appName, userID, sessionID,
	)
	return err
}

func (s *SessionService) mergeScopedState(ctx context.Context, table, where string, args []any, delta map[string]any) error {
	query := fmt.Sprintf("SELECT state_json FROM %s WHERE %s", table, where)
	row := s.exec.QueryRowContext(ctx, query, args...)
	state, err := scanStateJSON(row)
	if errors.Is(err, store.ErrSessionNotFound) || (err != nil && strings.Contains(err.Error(), "no rows")) {
		state = map[string]any{}
	} else if err != nil {
		return err
	}
	for k, v := range delta {
		state[k] = v
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	now := formatTime(time.Now().UTC())
	switch table {
	case "hai_agent_user_state":
		_, err = s.exec.ExecContext(ctx, `
			INSERT INTO hai_agent_user_state (tenant_id, app_name, user_id, state_json, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(tenant_id, app_name, user_id) DO UPDATE SET
				state_json = excluded.state_json,
				updated_at = excluded.updated_at`,
			args[0], args[1], args[2], string(raw), now)
	default:
		_, err = s.exec.ExecContext(ctx, `
			INSERT INTO hai_agent_app_state (tenant_id, app_name, state_json, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(tenant_id, app_name) DO UPDATE SET
				state_json = excluded.state_json,
				updated_at = excluded.updated_at`,
			args[0], args[1], string(raw), now)
	}
	return err
}

func scanAgentSessionRow(row interface{ Scan(dest ...any) error }) (*store.AgentSession, error) {
	var (
		id, tenant, app, user, stateJSON, created, updated string
		subject                                            sql.NullString
	)
	if err := row.Scan(&id, &tenant, &app, &user, &subject, &stateJSON, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrSessionNotFound
		}
		return nil, fmt.Errorf("scan session: %w", err)
	}
	state := map[string]any{}
	if stateJSON != "" {
		_ = json.Unmarshal([]byte(stateJSON), &state)
	}
	createdAt, _ := parseTime(created)
	updatedAt, _ := parseTime(updated)
	sess := &store.AgentSession{
		ID: id, TenantID: tenant, AppName: app, UserID: user,
		State: state, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
	if subject.Valid {
		sess.Subject = subject.String
	}
	return sess, nil
}

func scanStateJSON(row interface{ Scan(dest ...any) error }) (map[string]any, error) {
	var raw sql.NullString
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if !raw.Valid || raw.String == "" {
		return map[string]any{}, nil
	}
	state := map[string]any{}
	if err := json.Unmarshal([]byte(raw.String), &state); err != nil {
		return nil, err
	}
	return state, nil
}
