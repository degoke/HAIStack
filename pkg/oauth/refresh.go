package oauth

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

const (
	defaultRefreshTokenTTL = 30 * 24 * time.Hour
	refreshTokenBytes      = 32
)

// MemoryRefreshStore is a process-local refresh token store.
type MemoryRefreshStore struct {
	mu      sync.Mutex
	records map[string]*RefreshRecord
	Now     func() time.Time
}

// NewMemoryRefreshStore returns an in-memory refresh token store.
func NewMemoryRefreshStore() *MemoryRefreshStore {
	return &MemoryRefreshStore{records: make(map[string]*RefreshRecord)}
}

func (s *MemoryRefreshStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *MemoryRefreshStore) Issue(record RefreshRecord) (string, error) {
	if s == nil {
		return "", fmt.Errorf("%w: refresh store is nil", ErrInvalidConfig)
	}
	token, err := randomURLSafe(refreshTokenBytes)
	if err != nil {
		return "", err
	}
	record.Token = token
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = s.now().Add(defaultRefreshTokenTTL)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records == nil {
		s.records = make(map[string]*RefreshRecord)
	}
	s.purgeLocked()
	copy := record
	s.records[token] = &copy
	return token, nil
}

func (s *MemoryRefreshStore) Rotate(token, clientID string) (*RefreshRecord, string, error) {
	if s == nil {
		return nil, "", fmt.Errorf("%w: refresh store is nil", ErrInvalidConfig)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	entry, ok := s.records[token]
	if !ok || entry.Revoked {
		return nil, "", fmt.Errorf("%w: unknown refresh token", ErrInvalidGrant)
	}
	if s.now().After(entry.ExpiresAt) {
		delete(s.records, token)
		return nil, "", fmt.Errorf("%w: refresh token expired", ErrInvalidGrant)
	}
	if entry.ClientID != clientID {
		return nil, "", fmt.Errorf("%w: client mismatch", ErrInvalidGrant)
	}
	entry.Revoked = true
	copy := *entry
	newToken, err := randomURLSafe(refreshTokenBytes)
	if err != nil {
		return nil, "", err
	}
	replacement := copy
	replacement.Token = newToken
	replacement.Revoked = false
	replacement.ExpiresAt = s.now().Add(defaultRefreshTokenTTL)
	s.records[newToken] = &replacement
	return &copy, newToken, nil
}

func (s *MemoryRefreshStore) Revoke(token string) error {
	if s == nil {
		return fmt.Errorf("%w: refresh store is nil", ErrInvalidConfig)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.records[token]
	if !ok {
		return nil
	}
	entry.Revoked = true
	return nil
}

func (s *MemoryRefreshStore) Lookup(token string) (*RefreshRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: refresh store is nil", ErrInvalidConfig)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	entry, ok := s.records[token]
	if !ok || entry.Revoked {
		return nil, fmt.Errorf("%w: unknown refresh token", ErrInvalidGrant)
	}
	if s.now().After(entry.ExpiresAt) {
		delete(s.records, token)
		return nil, fmt.Errorf("%w: refresh token expired", ErrInvalidGrant)
	}
	copy := *entry
	return &copy, nil
}

func (s *MemoryRefreshStore) purgeLocked() {
	now := s.now()
	for key, entry := range s.records {
		if entry == nil || now.After(entry.ExpiresAt) {
			delete(s.records, key)
		}
	}
}

// MemoryRevocationStore tracks revoked JTIs until expiry.
type MemoryRevocationStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
	Now     func() time.Time
}

// NewMemoryRevocationStore returns an in-memory revocation store.
func NewMemoryRevocationStore() *MemoryRevocationStore {
	return &MemoryRevocationStore{entries: make(map[string]time.Time)}
}

func (s *MemoryRevocationStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *MemoryRevocationStore) Revoke(tokenID, tokenType string, expiresAt time.Time) error {
	if s == nil {
		return fmt.Errorf("%w: revocation store is nil", ErrInvalidConfig)
	}
	if tokenID == "" {
		return fmt.Errorf("%w: token id required", ErrInvalidRequest)
	}
	_ = tokenType
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[string]time.Time)
	}
	s.purgeLocked()
	s.entries[tokenID] = expiresAt
	return nil
}

func (s *MemoryRevocationStore) IsRevoked(tokenID string) (bool, error) {
	if s == nil || tokenID == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	exp, ok := s.entries[tokenID]
	if !ok {
		return false, nil
	}
	if s.now().After(exp) {
		delete(s.entries, tokenID)
		return false, nil
	}
	return true, nil
}

func (s *MemoryRevocationStore) purgeLocked() {
	now := s.now()
	for key, expiry := range s.entries {
		if now.After(expiry) {
			delete(s.entries, key)
		}
	}
}

func scopeAllowsOffline(scope string) bool {
	return scopeHasSpecialty(scope, "offline_access")
}

func scopeAllowsOpenID(scope string) bool {
	return scopeHasSpecialty(scope, "openid")
}

func scopeHasSpecialty(scope, specialty string) bool {
	set, err := smart.ParseScopes(strings.TrimSpace(scope))
	if err != nil {
		return false
	}
	for _, sc := range set.Strings() {
		if sc == specialty {
			return true
		}
	}
	return false
}
