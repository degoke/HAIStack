package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/smart"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// AuthorizationStore persists OAuth authorization state in Postgres.
type AuthorizationStore struct {
	pool execer
	now  func() time.Time
}

// NewAuthorizationStore constructs a Postgres-backed AuthorizationStore.
func NewAuthorizationStore(pool *pgxpool.Pool) *AuthorizationStore {
	return &AuthorizationStore{pool: pool, now: time.Now}
}

func (s *AuthorizationStore) SaveAuthorizationCode(code string, entry oauth.AuthorizationCode) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode auth code: %w", err)
	}
	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO hai_oauth_auth_code (code, payload, expires_at)
		VALUES ($1, $2::jsonb, $3)
		ON CONFLICT (code) DO UPDATE SET payload = EXCLUDED.payload, expires_at = EXCLUDED.expires_at`,
		code, payload, entry.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("save auth code: %w", err)
	}
	return nil
}

func (s *AuthorizationStore) ConsumeAuthorizationCode(code string) (oauth.AuthorizationCode, bool) {
	now := s.now()
	var payload []byte
	err := s.pool.QueryRow(context.Background(), `
		DELETE FROM hai_oauth_auth_code
		WHERE code = $1 AND expires_at > $2
		RETURNING payload`, code, now,
	).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) || err != nil {
		return oauth.AuthorizationCode{}, false
	}
	var entry oauth.AuthorizationCode
	if err := json.Unmarshal(payload, &entry); err != nil {
		return oauth.AuthorizationCode{}, false
	}
	return entry, true
}

func (s *AuthorizationStore) SaveRefreshToken(token string, entry oauth.RefreshTokenEntry) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode refresh token: %w", err)
	}
	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO hai_oauth_refresh_token (token, payload, expires_at)
		VALUES ($1, $2::jsonb, $3)
		ON CONFLICT (token) DO UPDATE SET payload = EXCLUDED.payload, expires_at = EXCLUDED.expires_at`,
		token, payload, entry.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("save refresh token: %w", err)
	}
	return nil
}

func (s *AuthorizationStore) ConsumeRefreshToken(token string) (oauth.RefreshTokenEntry, bool) {
	now := s.now()
	var payload []byte
	err := s.pool.QueryRow(context.Background(), `
		DELETE FROM hai_oauth_refresh_token
		WHERE token = $1 AND expires_at > $2
		RETURNING payload`, token, now,
	).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) || err != nil {
		return oauth.RefreshTokenEntry{}, false
	}
	var entry oauth.RefreshTokenEntry
	if err := json.Unmarshal(payload, &entry); err != nil {
		return oauth.RefreshTokenEntry{}, false
	}
	return entry, true
}

func (s *AuthorizationStore) SavePendingAuthorization(id string, entry oauth.PendingAuthorization) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode pending auth: %w", err)
	}
	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO hai_oauth_pending_auth (id, payload, expires_at)
		VALUES ($1, $2::jsonb, $3)
		ON CONFLICT (id) DO UPDATE SET payload = EXCLUDED.payload, expires_at = EXCLUDED.expires_at`,
		id, payload, entry.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("save pending auth: %w", err)
	}
	return nil
}

func (s *AuthorizationStore) GetPendingAuthorization(id string) (oauth.PendingAuthorization, bool) {
	now := s.now()
	var payload []byte
	err := s.pool.QueryRow(context.Background(), `
		SELECT payload FROM hai_oauth_pending_auth
		WHERE id = $1 AND expires_at > $2`, id, now,
	).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) || err != nil {
		return oauth.PendingAuthorization{}, false
	}
	var entry oauth.PendingAuthorization
	if err := json.Unmarshal(payload, &entry); err != nil {
		return oauth.PendingAuthorization{}, false
	}
	return entry, true
}

func (s *AuthorizationStore) ConsumePendingAuthorization(id string) (oauth.PendingAuthorization, bool) {
	now := s.now()
	var payload []byte
	err := s.pool.QueryRow(context.Background(), `
		DELETE FROM hai_oauth_pending_auth
		WHERE id = $1 AND expires_at > $2
		RETURNING payload`, id, now,
	).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) || err != nil {
		return oauth.PendingAuthorization{}, false
	}
	var entry oauth.PendingAuthorization
	if err := json.Unmarshal(payload, &entry); err != nil {
		return oauth.PendingAuthorization{}, false
	}
	return entry, true
}

func (s *AuthorizationStore) DeleteRefreshTokenForClient(token, clientID string) bool {
	now := s.now()
	tag, err := s.pool.Exec(context.Background(), `
		DELETE FROM hai_oauth_refresh_token
		WHERE token = $1 AND expires_at > $2 AND payload->>'clientId' = $3`,
		token, now, clientID,
	)
	if err != nil {
		return false
	}
	return tag.RowsAffected() > 0
}

// ClientRegistry persists OAuth clients in Postgres.
type ClientRegistry struct {
	pool execer
}

// NewClientRegistry constructs a Postgres-backed ClientRegistry.
func NewClientRegistry(pool *pgxpool.Pool) *ClientRegistry {
	return &ClientRegistry{pool: pool}
}

func (s *ClientRegistry) Get(clientID string) (oauth.Client, bool) {
	var payload []byte
	err := s.pool.QueryRow(context.Background(), `
		SELECT payload FROM hai_oauth_client WHERE client_id = $1`, clientID,
	).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) || err != nil {
		return oauth.Client{}, false
	}
	var client oauth.Client
	if err := json.Unmarshal(payload, &client); err != nil {
		return oauth.Client{}, false
	}
	return client, true
}

func (s *ClientRegistry) Register(client oauth.Client) error {
	if err := oauth.PrepareClientSecret(&client); err != nil {
		return err
	}
	payload, err := json.Marshal(client)
	if err != nil {
		return fmt.Errorf("encode oauth client: %w", err)
	}
	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO hai_oauth_client (client_id, payload, updated_at)
		VALUES ($1, $2::jsonb, now())
		ON CONFLICT (client_id) DO UPDATE SET payload = EXCLUDED.payload, updated_at = now()`,
		client.ClientID, payload,
	)
	if err != nil {
		return fmt.Errorf("register oauth client: %w", err)
	}
	return nil
}

// ReplayStore persists backend assertion replay protection in Postgres.
type ReplayStore struct {
	pool execer
	now  func() time.Time
}

// NewReplayStore constructs a Postgres-backed ReplayStore.
func NewReplayStore(pool *pgxpool.Pool) *ReplayStore {
	return &ReplayStore{pool: pool, now: time.Now}
}

func (s *ReplayStore) CheckAndStore(jti string, expiresAt time.Time) error {
	if jti == "" {
		return fmt.Errorf("%w: jti required", smart.ErrReplay)
	}
	now := s.now()
	_, err := s.pool.Exec(context.Background(), `
		DELETE FROM hai_oauth_replay_jti WHERE expires_at <= $1`, now)
	if err != nil {
		return fmt.Errorf("purge replay jti: %w", err)
	}
	tag, err := s.pool.Exec(context.Background(), `
		INSERT INTO hai_oauth_replay_jti (jti, expires_at)
		VALUES ($1, $2)
		ON CONFLICT (jti) DO NOTHING`, jti, expiresAt)
	if err != nil {
		return fmt.Errorf("store replay jti: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: jti %q", smart.ErrReplay, jti)
	}
	return nil
}

// RevocationStore persists revoked access-token JTIs in Postgres.
type RevocationStore struct {
	pool execer
	now  func() time.Time
}

// NewRevocationStore constructs a Postgres-backed TokenRevocationStore.
func NewRevocationStore(pool *pgxpool.Pool) *RevocationStore {
	return &RevocationStore{pool: pool, now: time.Now}
}

func (s *RevocationStore) Revoke(jti string, expiresAt time.Time) error {
	if jti == "" {
		return fmt.Errorf("oauth: jti required")
	}
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO hai_oauth_revoked_jti (jti, expires_at)
		VALUES ($1, $2)
		ON CONFLICT (jti) DO UPDATE SET expires_at = EXCLUDED.expires_at`,
		jti, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("revoke jti: %w", err)
	}
	return nil
}

func (s *RevocationStore) IsRevoked(jti string) bool {
	now := s.now()
	var exists bool
	err := s.pool.QueryRow(context.Background(), `
		SELECT EXISTS(
			SELECT 1 FROM hai_oauth_revoked_jti
			WHERE jti = $1 AND expires_at > $2
		)`, jti, now,
	).Scan(&exists)
	return err == nil && exists
}

// Stores returns production OAuth stores backed by Postgres.
func Stores(pool *pgxpool.Pool) (oauth.AuthorizationStore, oauth.ClientRegistry, smart.ReplayStore, oauth.TokenRevocationStore) {
	return NewAuthorizationStore(pool),
		NewClientRegistry(pool),
		NewReplayStore(pool),
		NewRevocationStore(pool)
}

var _ oauth.AuthorizationStore = (*AuthorizationStore)(nil)
var _ oauth.ClientRegistry = (*ClientRegistry)(nil)
var _ smart.ReplayStore = (*ReplayStore)(nil)
var _ oauth.TokenRevocationStore = (*RevocationStore)(nil)
