package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

// AuthorizationStore persists OAuth authorization state in SQLite.
type SQLiteAuthorizationStore struct {
	db  *sql.DB
	Now func() time.Time
}

// NewSQLiteAuthorizationStore constructs a SQLite-backed AuthorizationStore.
func NewSQLiteAuthorizationStore(db *sql.DB) *SQLiteAuthorizationStore {
	return &SQLiteAuthorizationStore{db: db, Now: time.Now}
}

func (s *SQLiteAuthorizationStore) SaveAuthorizationCode(code string, entry oauth.AuthorizationCode) error {
	issuer, err := oauth.RequireBoundIssuer(entry.Issuer)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode auth code: %w", err)
	}
	_, err = s.db.ExecContext(context.Background(), `
		INSERT INTO hai_oauth_auth_code (code, issuer, payload, expires_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (issuer, code) DO UPDATE SET payload = excluded.payload, expires_at = excluded.expires_at`,
		code, issuer, payload, entry.ExpiresAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("save auth code: %w", err)
	}
	return nil
}

func (s *SQLiteAuthorizationStore) ConsumeAuthorizationCode(issuer, code string) (oauth.AuthorizationCode, bool) {
	issuer = oauth.NormalizeIssuerURL(issuer)
	if issuer == "" {
		return oauth.AuthorizationCode{}, false
	}
	var payload, expiresAt string
	err := s.db.QueryRowContext(context.Background(), `
		DELETE FROM hai_oauth_auth_code
		WHERE code = ? AND issuer = ?
		RETURNING payload, expires_at`, code, issuer,
	).Scan(&payload, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return oauth.AuthorizationCode{}, false
	}
	if !oauthExpiryValid(expiresAt, s.clock()) {
		return oauth.AuthorizationCode{}, false
	}
	var entry oauth.AuthorizationCode
	if err := json.Unmarshal([]byte(payload), &entry); err != nil {
		return oauth.AuthorizationCode{}, false
	}
	return entry, true
}

func (s *SQLiteAuthorizationStore) SaveRefreshToken(token string, entry oauth.RefreshTokenEntry) error {
	issuer, err := oauth.RequireBoundIssuer(entry.Issuer)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode refresh token: %w", err)
	}
	_, err = s.db.ExecContext(context.Background(), `
		INSERT INTO hai_oauth_refresh_token (token, issuer, payload, expires_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (issuer, token) DO UPDATE SET payload = excluded.payload, expires_at = excluded.expires_at`,
		token, issuer, payload, entry.ExpiresAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("save refresh token: %w", err)
	}
	return nil
}

func (s *SQLiteAuthorizationStore) ConsumeRefreshToken(issuer, token string) (oauth.RefreshTokenEntry, bool) {
	issuer = oauth.NormalizeIssuerURL(issuer)
	if issuer == "" {
		return oauth.RefreshTokenEntry{}, false
	}
	var payload, expiresAt string
	err := s.db.QueryRowContext(context.Background(), `
		DELETE FROM hai_oauth_refresh_token
		WHERE token = ? AND issuer = ?
		RETURNING payload, expires_at`, token, issuer,
	).Scan(&payload, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return oauth.RefreshTokenEntry{}, false
	}
	if !oauthExpiryValid(expiresAt, s.clock()) {
		return oauth.RefreshTokenEntry{}, false
	}
	var entry oauth.RefreshTokenEntry
	if err := json.Unmarshal([]byte(payload), &entry); err != nil {
		return oauth.RefreshTokenEntry{}, false
	}
	return entry, true
}

func (s *SQLiteAuthorizationStore) LookupRefreshToken(issuer, token string) (oauth.RefreshTokenEntry, bool) {
	issuer = oauth.NormalizeIssuerURL(issuer)
	if issuer == "" {
		return oauth.RefreshTokenEntry{}, false
	}
	var payload, expiresAt string
	err := s.db.QueryRowContext(context.Background(), `
		SELECT payload, expires_at FROM hai_oauth_refresh_token
		WHERE token = ? AND issuer = ?`, token, issuer,
	).Scan(&payload, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return oauth.RefreshTokenEntry{}, false
	}
	if !oauthExpiryValid(expiresAt, s.clock()) {
		return oauth.RefreshTokenEntry{}, false
	}
	var entry oauth.RefreshTokenEntry
	if err := json.Unmarshal([]byte(payload), &entry); err != nil {
		return oauth.RefreshTokenEntry{}, false
	}
	return entry, true
}

func (s *SQLiteAuthorizationStore) SavePendingAuthorization(id string, entry oauth.PendingAuthorization) error {
	issuer, err := oauth.RequireBoundIssuer(entry.Issuer)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode pending auth: %w", err)
	}
	_, err = s.db.ExecContext(context.Background(), `
		INSERT INTO hai_oauth_pending_auth (id, issuer, payload, expires_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (issuer, id) DO UPDATE SET payload = excluded.payload, expires_at = excluded.expires_at`,
		id, issuer, payload, entry.ExpiresAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("save pending auth: %w", err)
	}
	return nil
}

func (s *SQLiteAuthorizationStore) GetPendingAuthorization(issuer, id string) (oauth.PendingAuthorization, bool) {
	issuer = oauth.NormalizeIssuerURL(issuer)
	if issuer == "" {
		return oauth.PendingAuthorization{}, false
	}
	var payload, expiresAt string
	err := s.db.QueryRowContext(context.Background(), `
		SELECT payload, expires_at FROM hai_oauth_pending_auth
		WHERE id = ? AND issuer = ?`, id, issuer,
	).Scan(&payload, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return oauth.PendingAuthorization{}, false
	}
	if !oauthExpiryValid(expiresAt, s.clock()) {
		return oauth.PendingAuthorization{}, false
	}
	var entry oauth.PendingAuthorization
	if err := json.Unmarshal([]byte(payload), &entry); err != nil {
		return oauth.PendingAuthorization{}, false
	}
	return entry, true
}

func (s *SQLiteAuthorizationStore) PurgeExpiredPendingAuthorizations() int {
	now := s.clock()
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT issuer, id, expires_at FROM hai_oauth_pending_auth`)
	if err != nil {
		return 0
	}
	defer func() { _ = rows.Close() }()
	deleted := 0
	for rows.Next() {
		var issuer, id, expiresAt string
		if err := rows.Scan(&issuer, &id, &expiresAt); err != nil {
			return deleted
		}
		if oauthExpiryValid(expiresAt, now) {
			continue
		}
		res, err := s.db.ExecContext(context.Background(), `
			DELETE FROM hai_oauth_pending_auth WHERE issuer = ? AND id = ?`, issuer, id)
		if err != nil {
			continue
		}
		n, _ := res.RowsAffected()
		deleted += int(n)
	}
	return deleted
}

func (s *SQLiteAuthorizationStore) ConsumePendingAuthorization(issuer, id string) (oauth.PendingAuthorization, bool) {
	issuer = oauth.NormalizeIssuerURL(issuer)
	if issuer == "" {
		return oauth.PendingAuthorization{}, false
	}
	var payload, expiresAt string
	err := s.db.QueryRowContext(context.Background(), `
		DELETE FROM hai_oauth_pending_auth
		WHERE id = ? AND issuer = ?
		RETURNING payload, expires_at`, id, issuer,
	).Scan(&payload, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return oauth.PendingAuthorization{}, false
	}
	if !oauthExpiryValid(expiresAt, s.clock()) {
		return oauth.PendingAuthorization{}, false
	}
	var entry oauth.PendingAuthorization
	if err := json.Unmarshal([]byte(payload), &entry); err != nil {
		return oauth.PendingAuthorization{}, false
	}
	return entry, true
}

func (s *SQLiteAuthorizationStore) DeleteRefreshTokenForClient(issuer, token, clientID string) bool {
	issuer = oauth.NormalizeIssuerURL(issuer)
	if issuer == "" {
		return false
	}
	var expiresAt string
	err := s.db.QueryRowContext(context.Background(), `
		DELETE FROM hai_oauth_refresh_token
		WHERE token = ? AND issuer = ? AND json_extract(payload, '$.clientId') = ?
		RETURNING expires_at`, token, issuer, clientID,
	).Scan(&expiresAt)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return false
	}
	return oauthExpiryValid(expiresAt, s.clock())
}

// SQLiteClientRegistry persists OAuth clients in SQLite.
type SQLiteClientRegistry struct {
	db     *sql.DB
	issuer string
}

// NewSQLiteClientRegistry constructs a SQLite-backed ClientRegistry.
func NewSQLiteClientRegistry(db *sql.DB) *SQLiteClientRegistry {
	return &SQLiteClientRegistry{db: db}
}

// ForIssuer returns a registry view scoped to issuer.
func (s *SQLiteClientRegistry) ForIssuer(issuer string) oauth.ClientRegistry {
	if s == nil {
		return NewSQLiteClientRegistry(nil)
	}
	cp := *s
	cp.issuer = oauth.NormalizeIssuerURL(issuer)
	return &cp
}

func (s *SQLiteClientRegistry) Get(clientID string) (oauth.Client, bool) {
	var payload string
	err := s.db.QueryRowContext(context.Background(), `
		SELECT payload FROM hai_oauth_client WHERE client_id = ? AND issuer = ?`, clientID, s.issuer,
	).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return oauth.Client{}, false
	}
	var client oauth.Client
	if err := json.Unmarshal([]byte(payload), &client); err != nil {
		return oauth.Client{}, false
	}
	return client, true
}

func (s *SQLiteClientRegistry) Register(client oauth.Client) error {
	if err := oauth.PrepareClientSecret(&client); err != nil {
		return err
	}
	payload, err := json.Marshal(client)
	if err != nil {
		return fmt.Errorf("encode oauth client: %w", err)
	}
	_, err = s.db.ExecContext(context.Background(), `
		INSERT INTO hai_oauth_client (issuer, client_id, payload, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (issuer, client_id) DO UPDATE SET payload = excluded.payload, updated_at = excluded.updated_at`,
		s.issuer, client.ClientID, payload, time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("register oauth client: %w", err)
	}
	return nil
}

// SQLiteReplayStore persists backend assertion replay protection in SQLite.
type SQLiteReplayStore struct {
	db  *sql.DB
	now func() time.Time
}

// NewSQLiteReplayStore constructs a SQLite-backed ReplayStore.
func NewSQLiteReplayStore(db *sql.DB) *SQLiteReplayStore {
	return &SQLiteReplayStore{db: db, now: time.Now}
}

func (s *SQLiteReplayStore) CheckAndStore(jti string, expiresAt time.Time) error {
	if jti == "" {
		return fmt.Errorf("%w: jti required", smart.ErrReplay)
	}
	now := s.clock()
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT jti, expires_at FROM hai_oauth_replay_jti`)
	if err != nil {
		return fmt.Errorf("purge replay jti: %w", err)
	}
	var expired []string
	for rows.Next() {
		var id, exp string
		if err := rows.Scan(&id, &exp); err != nil {
			_ = rows.Close()
			return fmt.Errorf("purge replay jti: %w", err)
		}
		if !oauthExpiryValid(exp, now) {
			expired = append(expired, id)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("purge replay jti: %w", err)
	}
	_ = rows.Close()
	for _, id := range expired {
		if _, err := s.db.ExecContext(context.Background(), `
			DELETE FROM hai_oauth_replay_jti WHERE jti = ?`, id); err != nil {
			return fmt.Errorf("purge replay jti: %w", err)
		}
	}
	res, err := s.db.ExecContext(context.Background(), `
		INSERT OR IGNORE INTO hai_oauth_replay_jti (jti, expires_at)
		VALUES (?, ?)`, jti, expiresAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("store replay jti: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: jti %q", smart.ErrReplay, jti)
	}
	return nil
}

// SQLiteRevocationStore persists revoked access-token JTIs in SQLite.
type SQLiteRevocationStore struct {
	db  *sql.DB
	now func() time.Time
}

// NewSQLiteRevocationStore constructs a SQLite-backed TokenRevocationStore.
func NewSQLiteRevocationStore(db *sql.DB) *SQLiteRevocationStore {
	return &SQLiteRevocationStore{db: db, now: time.Now}
}

func (s *SQLiteRevocationStore) Revoke(jti string, expiresAt time.Time) error {
	if jti == "" {
		return fmt.Errorf("oauth: jti required")
	}
	_, err := s.db.ExecContext(context.Background(), `
		INSERT INTO hai_oauth_revoked_jti (jti, expires_at)
		VALUES (?, ?)
		ON CONFLICT (jti) DO UPDATE SET expires_at = excluded.expires_at`,
		jti, expiresAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("revoke jti: %w", err)
	}
	return nil
}

func (s *SQLiteRevocationStore) IsRevoked(jti string) bool {
	var expiresAt string
	err := s.db.QueryRowContext(context.Background(), `
		SELECT expires_at FROM hai_oauth_revoked_jti
		WHERE jti = ?
		LIMIT 1`, jti,
	).Scan(&expiresAt)
	if err != nil {
		return false
	}
	return oauthExpiryValid(expiresAt, s.clock())
}

func (s *SQLiteAuthorizationStore) clock() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *SQLiteReplayStore) clock() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *SQLiteRevocationStore) clock() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func oauthExpiryValid(raw string, now time.Time) bool {
	exp, err := parseOAuthExpiry(raw)
	return err == nil && exp.After(now.UTC())
}

func parseOAuthExpiry(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty expiry")
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("parse expiry %q", raw)
}

// SQLiteStores returns production OAuth stores backed by SQLite.
func SQLiteStores(db *sql.DB) (oauth.AuthorizationStore, oauth.ClientRegistry, smart.ReplayStore, oauth.TokenRevocationStore) {
	return NewSQLiteAuthorizationStore(db),
		NewSQLiteClientRegistry(db),
		NewSQLiteReplayStore(db),
		NewSQLiteRevocationStore(db)
}

var _ oauth.AuthorizationStore = (*SQLiteAuthorizationStore)(nil)
var _ oauth.ClientRegistry = (*SQLiteClientRegistry)(nil)
var _ smart.ReplayStore = (*SQLiteReplayStore)(nil)
var _ oauth.TokenRevocationStore = (*SQLiteRevocationStore)(nil)
