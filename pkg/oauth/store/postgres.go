package store

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
	issuer, err := oauth.RequireBoundIssuer(entry.Issuer)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode auth code: %w", err)
	}
	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO hai_oauth_auth_code (code, issuer, payload, expires_at)
		VALUES ($1, $2, $3::jsonb, $4)
		ON CONFLICT (issuer, code) DO UPDATE SET payload = EXCLUDED.payload, expires_at = EXCLUDED.expires_at`,
		code, issuer, payload, entry.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("save auth code: %w", err)
	}
	return nil
}

func (s *AuthorizationStore) ConsumeAuthorizationCode(issuer, code string) (oauth.AuthorizationCode, bool) {
	issuer = oauth.NormalizeIssuerURL(issuer)
	if issuer == "" {
		return oauth.AuthorizationCode{}, false
	}
	now := s.now()
	var payload []byte
	err := s.pool.QueryRow(context.Background(), `
		DELETE FROM hai_oauth_auth_code
		WHERE code = $1 AND expires_at > $2 AND issuer = $3
		RETURNING payload`, code, now, issuer,
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
	issuer, err := oauth.RequireBoundIssuer(entry.Issuer)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode refresh token: %w", err)
	}
	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO hai_oauth_refresh_token (token, issuer, payload, expires_at)
		VALUES ($1, $2, $3::jsonb, $4)
		ON CONFLICT (issuer, token) DO UPDATE SET payload = EXCLUDED.payload, expires_at = EXCLUDED.expires_at`,
		token, issuer, payload, entry.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("save refresh token: %w", err)
	}
	return nil
}

func (s *AuthorizationStore) ConsumeRefreshToken(issuer, token string) (oauth.RefreshTokenEntry, bool) {
	issuer = oauth.NormalizeIssuerURL(issuer)
	if issuer == "" {
		return oauth.RefreshTokenEntry{}, false
	}
	now := s.now()
	var payload []byte
	err := s.pool.QueryRow(context.Background(), `
		DELETE FROM hai_oauth_refresh_token
		WHERE token = $1 AND expires_at > $2 AND issuer = $3
		RETURNING payload`, token, now, issuer,
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

func (s *AuthorizationStore) LookupRefreshToken(issuer, token string) (oauth.RefreshTokenEntry, bool) {
	issuer = oauth.NormalizeIssuerURL(issuer)
	if issuer == "" {
		return oauth.RefreshTokenEntry{}, false
	}
	now := s.now()
	var payload []byte
	err := s.pool.QueryRow(context.Background(), `
		SELECT payload FROM hai_oauth_refresh_token
		WHERE token = $1 AND expires_at > $2 AND issuer = $3`, token, now, issuer,
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
	issuer, err := oauth.RequireBoundIssuer(entry.Issuer)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode pending auth: %w", err)
	}
	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO hai_oauth_pending_auth (id, issuer, payload, expires_at)
		VALUES ($1, $2, $3::jsonb, $4)
		ON CONFLICT (issuer, id) DO UPDATE SET payload = EXCLUDED.payload, expires_at = EXCLUDED.expires_at`,
		id, issuer, payload, entry.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("save pending auth: %w", err)
	}
	return nil
}

func (s *AuthorizationStore) GetPendingAuthorization(issuer, id string) (oauth.PendingAuthorization, bool) {
	issuer = oauth.NormalizeIssuerURL(issuer)
	if issuer == "" {
		return oauth.PendingAuthorization{}, false
	}
	now := s.now()
	var payload []byte
	err := s.pool.QueryRow(context.Background(), `
		SELECT payload FROM hai_oauth_pending_auth
		WHERE id = $1 AND expires_at > $2 AND issuer = $3`, id, now, issuer,
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

func (s *AuthorizationStore) PurgeExpiredPendingAuthorizations() int {
	now := s.now()
	tag, err := s.pool.Exec(context.Background(), `
		DELETE FROM hai_oauth_pending_auth WHERE expires_at <= $1`, now)
	if err != nil {
		return 0
	}
	return int(tag.RowsAffected())
}

func (s *AuthorizationStore) ConsumePendingAuthorization(issuer, id string) (oauth.PendingAuthorization, bool) {
	issuer = oauth.NormalizeIssuerURL(issuer)
	if issuer == "" {
		return oauth.PendingAuthorization{}, false
	}
	now := s.now()
	var payload []byte
	err := s.pool.QueryRow(context.Background(), `
		DELETE FROM hai_oauth_pending_auth
		WHERE id = $1 AND expires_at > $2 AND issuer = $3
		RETURNING payload`, id, now, issuer,
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

func (s *AuthorizationStore) DeleteRefreshTokenForClient(issuer, token, clientID string) bool {
	issuer = oauth.NormalizeIssuerURL(issuer)
	if issuer == "" {
		return false
	}
	now := s.now()
	tag, err := s.pool.Exec(context.Background(), `
		DELETE FROM hai_oauth_refresh_token
		WHERE token = $1 AND expires_at > $2 AND issuer = $3 AND payload->>'clientId' = $4`,
		token, now, issuer, clientID,
	)
	if err != nil {
		return false
	}
	return tag.RowsAffected() > 0
}

// ClientRegistry persists OAuth clients in Postgres.
type ClientRegistry struct {
	pool   execer
	issuer string
}

// NewClientRegistry constructs a Postgres-backed ClientRegistry.
func NewClientRegistry(pool *pgxpool.Pool) *ClientRegistry {
	return &ClientRegistry{pool: pool}
}

// ForIssuer returns a registry view scoped to issuer.
func (s *ClientRegistry) ForIssuer(issuer string) oauth.ClientRegistry {
	if s == nil {
		return NewClientRegistry(nil)
	}
	cp := *s
	cp.issuer = oauth.NormalizeIssuerURL(issuer)
	return &cp
}

func (s *ClientRegistry) Get(clientID string) (oauth.Client, bool) {
	var payload []byte
	err := s.pool.QueryRow(context.Background(), `
		SELECT payload FROM hai_oauth_client WHERE client_id = $1 AND issuer = $2`, clientID, s.issuer,
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
		INSERT INTO hai_oauth_client (issuer, client_id, payload, updated_at)
		VALUES ($1, $2, $3::jsonb, now())
		ON CONFLICT (issuer, client_id) DO UPDATE SET payload = EXCLUDED.payload, updated_at = now()`,
		s.issuer, client.ClientID, payload,
	)
	if err != nil {
		return fmt.Errorf("register oauth client: %w", err)
	}
	return nil
}

func bindUnscopedPostgresClients(pool *pgxpool.Pool, issuers ...string) error {
	if pool == nil {
		return nil
	}
	dest := uniqueNormalizedIssuers(issuers...)
	if len(dest) == 0 {
		return nil
	}
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("bind unscoped oauth clients: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, issuer := range dest {
		if _, err := tx.Exec(ctx, `
			INSERT INTO hai_oauth_client (issuer, client_id, payload, updated_at)
			SELECT $1, client_id, payload, updated_at FROM hai_oauth_client WHERE issuer = ''
			ON CONFLICT (issuer, client_id) DO NOTHING`, issuer); err != nil {
			return fmt.Errorf("bind unscoped oauth clients: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM hai_oauth_client WHERE issuer = ''`); err != nil {
		return fmt.Errorf("bind unscoped oauth clients: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("bind unscoped oauth clients: %w", err)
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

// PostgresStores returns production OAuth stores backed by Postgres.
func PostgresStores(pool *pgxpool.Pool) (oauth.AuthorizationStore, oauth.ClientRegistry, smart.ReplayStore, oauth.TokenRevocationStore) {
	return NewAuthorizationStore(pool),
		NewClientRegistry(pool),
		NewReplayStore(pool),
		NewRevocationStore(pool)
}

var _ oauth.AuthorizationStore = (*AuthorizationStore)(nil)
var _ oauth.ClientRegistry = (*ClientRegistry)(nil)
var _ smart.ReplayStore = (*ReplayStore)(nil)
var _ oauth.TokenRevocationStore = (*RevocationStore)(nil)
