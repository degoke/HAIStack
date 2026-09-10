package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/smart"
	goredis "github.com/redis/go-redis/v9"
)

const defaultKeyPrefix = "hai:oauth:"

// AuthorizationStore persists OAuth authorization state in Redis.
type AuthorizationStore struct {
	client goredis.Cmdable
	prefix string
	now    func() time.Time
}

// NewAuthorizationStore constructs a Redis-backed AuthorizationStore.
func NewAuthorizationStore(client goredis.Cmdable, keyPrefix string) *AuthorizationStore {
	if keyPrefix == "" {
		keyPrefix = defaultKeyPrefix
	}
	return &AuthorizationStore{client: client, prefix: keyPrefix, now: time.Now}
}

func (s *AuthorizationStore) key(parts ...string) string {
	return s.prefix + parts[0]
}

func (s *AuthorizationStore) SaveAuthorizationCode(code string, entry oauth.AuthorizationCode) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode auth code: %w", err)
	}
	ttl := ttlUntil(entry.ExpiresAt, s.now())
	return s.client.Set(context.Background(), s.key("authcode:"+code), payload, ttl).Err()
}

func (s *AuthorizationStore) ConsumeAuthorizationCode(code string) (oauth.AuthorizationCode, bool) {
	key := s.key("authcode:" + code)
	payload, err := s.client.GetDel(context.Background(), key).Bytes()
	if err == goredis.Nil || err != nil {
		return oauth.AuthorizationCode{}, false
	}
	var entry oauth.AuthorizationCode
	if err := json.Unmarshal(payload, &entry); err != nil {
		return oauth.AuthorizationCode{}, false
	}
	if s.now().After(entry.ExpiresAt) {
		return oauth.AuthorizationCode{}, false
	}
	return entry, true
}

func (s *AuthorizationStore) SaveRefreshToken(token string, entry oauth.RefreshTokenEntry) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode refresh token: %w", err)
	}
	ttl := ttlUntil(entry.ExpiresAt, s.now())
	return s.client.Set(context.Background(), s.key("refresh:"+token), payload, ttl).Err()
}

func (s *AuthorizationStore) ConsumeRefreshToken(token string) (oauth.RefreshTokenEntry, bool) {
	key := s.key("refresh:" + token)
	payload, err := s.client.GetDel(context.Background(), key).Bytes()
	if err == goredis.Nil || err != nil {
		return oauth.RefreshTokenEntry{}, false
	}
	var entry oauth.RefreshTokenEntry
	if err := json.Unmarshal(payload, &entry); err != nil {
		return oauth.RefreshTokenEntry{}, false
	}
	if s.now().After(entry.ExpiresAt) {
		return oauth.RefreshTokenEntry{}, false
	}
	return entry, true
}

func (s *AuthorizationStore) SavePendingAuthorization(id string, entry oauth.PendingAuthorization) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode pending auth: %w", err)
	}
	ttl := ttlUntil(entry.ExpiresAt, s.now())
	return s.client.Set(context.Background(), s.key("pending:"+id), payload, ttl).Err()
}

func (s *AuthorizationStore) GetPendingAuthorization(id string) (oauth.PendingAuthorization, bool) {
	payload, err := s.client.Get(context.Background(), s.key("pending:"+id)).Bytes()
	if err == goredis.Nil || err != nil {
		return oauth.PendingAuthorization{}, false
	}
	var entry oauth.PendingAuthorization
	if err := json.Unmarshal(payload, &entry); err != nil {
		return oauth.PendingAuthorization{}, false
	}
	if s.now().After(entry.ExpiresAt) {
		return oauth.PendingAuthorization{}, false
	}
	return entry, true
}

func (s *AuthorizationStore) ConsumePendingAuthorization(id string) (oauth.PendingAuthorization, bool) {
	key := s.key("pending:" + id)
	payload, err := s.client.GetDel(context.Background(), key).Bytes()
	if err == goredis.Nil || err != nil {
		return oauth.PendingAuthorization{}, false
	}
	var entry oauth.PendingAuthorization
	if err := json.Unmarshal(payload, &entry); err != nil {
		return oauth.PendingAuthorization{}, false
	}
	if s.now().After(entry.ExpiresAt) {
		return oauth.PendingAuthorization{}, false
	}
	return entry, true
}

func (s *AuthorizationStore) DeleteRefreshTokenForClient(token, clientID string) bool {
	key := s.key("refresh:" + token)
	payload, err := s.client.Get(context.Background(), key).Bytes()
	if err == goredis.Nil || err != nil {
		return false
	}
	var entry oauth.RefreshTokenEntry
	if err := json.Unmarshal(payload, &entry); err != nil {
		return false
	}
	if entry.ClientID != clientID || s.now().After(entry.ExpiresAt) {
		return false
	}
	n, err := s.client.Del(context.Background(), key).Result()
	return err == nil && n > 0
}

// ReplayStore persists backend assertion replay protection in Redis.
type ReplayStore struct {
	client goredis.Cmdable
	prefix string
	now    func() time.Time
}

// NewReplayStore constructs a Redis-backed ReplayStore.
func NewReplayStore(client goredis.Cmdable, keyPrefix string) *ReplayStore {
	if keyPrefix == "" {
		keyPrefix = defaultKeyPrefix
	}
	return &ReplayStore{client: client, prefix: keyPrefix, now: time.Now}
}

func (s *ReplayStore) key(jti string) string {
	return s.prefix + "replay:" + jti
}

func (s *ReplayStore) CheckAndStore(jti string, expiresAt time.Time) error {
	if jti == "" {
		return fmt.Errorf("%w: jti required", smart.ErrReplay)
	}
	ttl := ttlUntil(expiresAt, s.now())
	ok, err := s.client.SetNX(context.Background(), s.key(jti), "1", ttl).Result()
	if err != nil {
		return fmt.Errorf("store replay jti: %w", err)
	}
	if !ok {
		return fmt.Errorf("%w: jti %q", smart.ErrReplay, jti)
	}
	return nil
}

// RevocationStore persists revoked access-token JTIs in Redis.
type RevocationStore struct {
	client goredis.Cmdable
	prefix string
	now    func() time.Time
}

// NewRevocationStore constructs a Redis-backed TokenRevocationStore.
func NewRevocationStore(client goredis.Cmdable, keyPrefix string) *RevocationStore {
	if keyPrefix == "" {
		keyPrefix = defaultKeyPrefix
	}
	return &RevocationStore{client: client, prefix: keyPrefix, now: time.Now}
}

func (s *RevocationStore) key(jti string) string {
	return s.prefix + "revoked:" + jti
}

func (s *RevocationStore) Revoke(jti string, expiresAt time.Time) error {
	if jti == "" {
		return fmt.Errorf("oauth: jti required")
	}
	ttl := ttlUntil(expiresAt, s.now())
	return s.client.Set(context.Background(), s.key(jti), "1", ttl).Err()
}

func (s *RevocationStore) IsRevoked(jti string) bool {
	n, err := s.client.Exists(context.Background(), s.key(jti)).Result()
	return err == nil && n > 0
}

// EphemeralStores returns Redis-backed token, replay, and revocation stores.
// Client registration must use a durable registry (Postgres or file) via oauth.Config.Clients.
func EphemeralStores(client goredis.Cmdable, keyPrefix string) (oauth.AuthorizationStore, smart.ReplayStore, oauth.TokenRevocationStore) {
	return NewAuthorizationStore(client, keyPrefix),
		NewReplayStore(client, keyPrefix),
		NewRevocationStore(client, keyPrefix)
}

func ttlUntil(expiresAt time.Time, now time.Time) time.Duration {
	ttl := expiresAt.Sub(now)
	if ttl <= 0 {
		return time.Second
	}
	return ttl
}

var _ oauth.AuthorizationStore = (*AuthorizationStore)(nil)
var _ smart.ReplayStore = (*ReplayStore)(nil)
var _ oauth.TokenRevocationStore = (*RevocationStore)(nil)
