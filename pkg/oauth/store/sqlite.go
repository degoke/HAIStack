package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

// AuthorizationStore persists OAuth authorization state in SQLite.
type SQLiteAuthorizationStore struct {
	db  *sql.DB
	now func() time.Time
}

// NewSQLiteAuthorizationStore constructs a SQLite-backed AuthorizationStore.
func NewSQLiteAuthorizationStore(db *sql.DB) *SQLiteAuthorizationStore {
	return &SQLiteAuthorizationStore{db: db, now: time.Now}
}

func (s *SQLiteAuthorizationStore) SaveAuthorizationCode(code string, entry oauth.AuthorizationCode) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode auth code: %w", err)
	}
	_, err = s.db.ExecContext(context.Background(), `
		INSERT INTO hai_oauth_auth_code (code, payload, expires_at)
		VALUES (?, ?, ?)
		ON CONFLICT (code) DO UPDATE SET payload = excluded.payload, expires_at = excluded.expires_at`,
		code, payload, entry.ExpiresAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("save auth code: %w", err)
	}
	return nil
}

func (s *SQLiteAuthorizationStore) ConsumeAuthorizationCode(code string) (oauth.AuthorizationCode, bool) {
	now := s.now().UTC().Format(time.RFC3339Nano)
	var payload string
	err := s.db.QueryRowContext(context.Background(), `
		DELETE FROM hai_oauth_auth_code
		WHERE code = ? AND expires_at > ?
		RETURNING payload`, code, now,
	).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return oauth.AuthorizationCode{}, false
	}
	var entry oauth.AuthorizationCode
	if err := json.Unmarshal([]byte(payload), &entry); err != nil {
		return oauth.AuthorizationCode{}, false
	}
	return entry, true
}

func (s *SQLiteAuthorizationStore) SaveRefreshToken(token string, entry oauth.RefreshTokenEntry) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode refresh token: %w", err)
	}
	_, err = s.db.ExecContext(context.Background(), `
		INSERT INTO hai_oauth_refresh_token (token, payload, expires_at)
		VALUES (?, ?, ?)
		ON CONFLICT (token) DO UPDATE SET payload = excluded.payload, expires_at = excluded.expires_at`,
		token, payload, entry.ExpiresAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("save refresh token: %w", err)
	}
	return nil
}

func (s *SQLiteAuthorizationStore) ConsumeRefreshToken(token string) (oauth.RefreshTokenEntry, bool) {
	entry, ok := s.LookupRefreshToken(token)
	if !ok {
		return oauth.RefreshTokenEntry{}, false
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(context.Background(), `
		DELETE FROM hai_oauth_refresh_token
		WHERE token = ? AND expires_at > ?`, token, now)
	if err != nil {
		return oauth.RefreshTokenEntry{}, false
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return oauth.RefreshTokenEntry{}, false
	}
	return entry, true
}

func (s *SQLiteAuthorizationStore) LookupRefreshToken(token string) (oauth.RefreshTokenEntry, bool) {
	now := s.now().UTC().Format(time.RFC3339Nano)
	var payload string
	err := s.db.QueryRowContext(context.Background(), `
		SELECT payload FROM hai_oauth_refresh_token
		WHERE token = ? AND expires_at > ?`, token, now,
	).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return oauth.RefreshTokenEntry{}, false
	}
	var entry oauth.RefreshTokenEntry
	if err := json.Unmarshal([]byte(payload), &entry); err != nil {
		return oauth.RefreshTokenEntry{}, false
	}
	return entry, true
}

func (s *SQLiteAuthorizationStore) SavePendingAuthorization(id string, entry oauth.PendingAuthorization) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode pending auth: %w", err)
	}
	_, err = s.db.ExecContext(context.Background(), `
		INSERT INTO hai_oauth_pending_auth (id, payload, expires_at)
		VALUES (?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET payload = excluded.payload, expires_at = excluded.expires_at`,
		id, payload, entry.ExpiresAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("save pending auth: %w", err)
	}
	return nil
}

func (s *SQLiteAuthorizationStore) GetPendingAuthorization(id string) (oauth.PendingAuthorization, bool) {
	now := s.now().UTC().Format(time.RFC3339Nano)
	var payload string
	err := s.db.QueryRowContext(context.Background(), `
		SELECT payload FROM hai_oauth_pending_auth
		WHERE id = ? AND expires_at > ?`, id, now,
	).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return oauth.PendingAuthorization{}, false
	}
	var entry oauth.PendingAuthorization
	if err := json.Unmarshal([]byte(payload), &entry); err != nil {
		return oauth.PendingAuthorization{}, false
	}
	return entry, true
}

func (s *SQLiteAuthorizationStore) ConsumePendingAuthorization(id string) (oauth.PendingAuthorization, bool) {
	now := s.now().UTC().Format(time.RFC3339Nano)
	var payload string
	err := s.db.QueryRowContext(context.Background(), `
		DELETE FROM hai_oauth_pending_auth
		WHERE id = ? AND expires_at > ?
		RETURNING payload`, id, now,
	).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return oauth.PendingAuthorization{}, false
	}
	var entry oauth.PendingAuthorization
	if err := json.Unmarshal([]byte(payload), &entry); err != nil {
		return oauth.PendingAuthorization{}, false
	}
	return entry, true
}

func (s *SQLiteAuthorizationStore) DeleteRefreshTokenForClient(token, clientID string) bool {
	now := s.now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(context.Background(), `
		DELETE FROM hai_oauth_refresh_token
		WHERE token = ? AND expires_at > ? AND json_extract(payload, '$.clientId') = ?`,
		token, now, clientID,
	)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// SQLiteClientRegistry persists OAuth clients in SQLite.
type SQLiteClientRegistry struct {
	db *sql.DB
}

// NewSQLiteClientRegistry constructs a SQLite-backed ClientRegistry.
func NewSQLiteClientRegistry(db *sql.DB) *SQLiteClientRegistry {
	return &SQLiteClientRegistry{db: db}
}

func (s *SQLiteClientRegistry) Get(clientID string) (oauth.Client, bool) {
	var payload string
	err := s.db.QueryRowContext(context.Background(), `
		SELECT payload FROM hai_oauth_client WHERE client_id = ?`, clientID,
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
		INSERT INTO hai_oauth_client (client_id, payload, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT (client_id) DO UPDATE SET payload = excluded.payload, updated_at = excluded.updated_at`,
		client.ClientID, payload, time.Now().UTC().Format(time.RFC3339Nano),
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
	now := s.now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(context.Background(), `
		DELETE FROM hai_oauth_replay_jti WHERE expires_at <= ?`, now)
	if err != nil {
		return fmt.Errorf("purge replay jti: %w", err)
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
	now := s.now().UTC().Format(time.RFC3339Nano)
	var exists int
	err := s.db.QueryRowContext(context.Background(), `
		SELECT 1 FROM hai_oauth_revoked_jti
		WHERE jti = ? AND expires_at > ?
		LIMIT 1`, jti, now,
	).Scan(&exists)
	return err == nil
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
