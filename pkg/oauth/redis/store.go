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

func (s *AuthorizationStore) SaveAuthorizationCode(code string, entry oauth.AuthorizationCode) error {
	iss, err := oauth.RequireBoundIssuer(entry.Issuer)
	if err != nil {
		return err
	}
	entry.Issuer = iss
	key, err := authCodeRedisKey(s.prefix, iss, code)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode auth code: %w", err)
	}
	ttl := ttlUntil(entry.ExpiresAt, s.now())
	return s.client.Set(context.Background(), key, payload, ttl).Err()
}

func (s *AuthorizationStore) ConsumeAuthorizationCode(issuer, code string) (oauth.AuthorizationCode, bool) {
	bound, err := oauth.RequireBoundIssuer(issuer)
	if err != nil {
		return oauth.AuthorizationCode{}, false
	}
	key, err := authCodeRedisKey(s.prefix, bound, code)
	if err != nil {
		return oauth.AuthorizationCode{}, false
	}
	payload, ok := consumeBoundJSONValue(s.client, key, bound)
	if !ok {
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
	iss, err := oauth.RequireBoundIssuer(entry.Issuer)
	if err != nil {
		return err
	}
	entry.Issuer = iss
	key, err := refreshRedisKey(s.prefix, iss, token)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode refresh token: %w", err)
	}
	ttl := ttlUntil(entry.ExpiresAt, s.now())
	_, err = saveRefreshTokenScript.Run(
		context.Background(),
		s.client,
		[]string{key},
		entry.ClientID,
		payload,
		int(ttl.Seconds()),
	).Result()
	return err
}

func (s *AuthorizationStore) ConsumeRefreshToken(issuer, token string) (oauth.RefreshTokenEntry, bool) {
	bound, err := oauth.RequireBoundIssuer(issuer)
	if err != nil {
		return oauth.RefreshTokenEntry{}, false
	}
	key, err := refreshRedisKey(s.prefix, bound, token)
	if err != nil {
		return oauth.RefreshTokenEntry{}, false
	}
	payload, ok := consumeBoundRefreshValue(s.client, key, bound)
	if !ok {
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

func (s *AuthorizationStore) LookupRefreshToken(issuer, token string) (oauth.RefreshTokenEntry, bool) {
	bound, err := oauth.RequireBoundIssuer(issuer)
	if err != nil {
		return oauth.RefreshTokenEntry{}, false
	}
	key, err := refreshRedisKey(s.prefix, bound, token)
	if err != nil {
		return oauth.RefreshTokenEntry{}, false
	}
	payload, err := s.client.HGet(context.Background(), key, "payload").Bytes()
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
	iss, err := oauth.RequireBoundIssuer(entry.Issuer)
	if err != nil {
		return err
	}
	entry.Issuer = iss
	key, err := pendingRedisKey(s.prefix, iss, id)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode pending auth: %w", err)
	}
	ttl := ttlUntil(entry.ExpiresAt, s.now())
	return s.client.Set(context.Background(), key, payload, ttl).Err()
}

func (s *AuthorizationStore) GetPendingAuthorization(issuer, id string) (oauth.PendingAuthorization, bool) {
	bound, err := oauth.RequireBoundIssuer(issuer)
	if err != nil {
		return oauth.PendingAuthorization{}, false
	}
	key, err := pendingRedisKey(s.prefix, bound, id)
	if err != nil {
		return oauth.PendingAuthorization{}, false
	}
	payload, err := s.client.Get(context.Background(), key).Bytes()
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

func (s *AuthorizationStore) PurgeExpiredPendingAuthorizations() int {
	return 0
}

func (s *AuthorizationStore) ConsumePendingAuthorization(issuer, id string) (oauth.PendingAuthorization, bool) {
	bound, err := oauth.RequireBoundIssuer(issuer)
	if err != nil {
		return oauth.PendingAuthorization{}, false
	}
	key, err := pendingRedisKey(s.prefix, bound, id)
	if err != nil {
		return oauth.PendingAuthorization{}, false
	}
	payload, ok := consumeBoundJSONValue(s.client, key, bound)
	if !ok {
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

func consumeBoundJSONValue(client goredis.Cmdable, key, issuer string) ([]byte, bool) {
	payload, err := consumeBoundJSONValueScript.Run(context.Background(), client, []string{key}, issuer).Text()
	if err != nil || payload == "" {
		return nil, false
	}
	return []byte(payload), true
}

func consumeBoundRefreshValue(client goredis.Cmdable, key, issuer string) ([]byte, bool) {
	payload, err := consumeBoundRefreshTokenScript.Run(context.Background(), client, []string{key}, issuer).Text()
	if err != nil || payload == "" {
		return nil, false
	}
	return []byte(payload), true
}

func (s *AuthorizationStore) DeleteRefreshTokenForClient(issuer, token, clientID string) bool {
	entry, ok := s.LookupRefreshToken(issuer, token)
	if !ok || entry.ClientID != clientID {
		return false
	}
	bound, err := oauth.RequireBoundIssuer(issuer)
	if err != nil {
		return false
	}
	key, err := refreshRedisKey(s.prefix, bound, token)
	if err != nil {
		return false
	}
	n, err := deleteRefreshTokenForClientScript.Run(context.Background(), s.client, []string{key}, clientID).Int()
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
