package oauth

import (
	"fmt"
	"sync"
	"time"
)

// TokenRevocationStore records revoked access-token JTIs until natural expiry.
type TokenRevocationStore interface {
	Revoke(jti string, expiresAt time.Time) error
	IsRevoked(jti string) bool
}

// MemoryTokenRevocationStore is a process-local revocation denylist.
type MemoryTokenRevocationStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
	Now     func() time.Time
}

// NewMemoryTokenRevocationStore constructs an in-memory revocation store.
func NewMemoryTokenRevocationStore() *MemoryTokenRevocationStore {
	return &MemoryTokenRevocationStore{entries: make(map[string]time.Time)}
}

func (s *MemoryTokenRevocationStore) Revoke(jti string, expiresAt time.Time) error {
	if s == nil || jti == "" {
		return fmt.Errorf("oauth: jti required")
	}
	s.mu.Lock()
	if s.entries == nil {
		s.entries = make(map[string]time.Time)
	}
	s.entries[jti] = expiresAt
	s.mu.Unlock()
	return nil
}

func (s *MemoryTokenRevocationStore) IsRevoked(jti string) bool {
	if s == nil || jti == "" {
		return false
	}
	now := revocationNow(s.Now)
	s.mu.Lock()
	expiry, ok := s.entries[jti]
	if ok && !expiry.IsZero() && now.After(expiry) {
		delete(s.entries, jti)
		ok = false
	}
	s.mu.Unlock()
	return ok
}

func revocationNow(nowFn func() time.Time) time.Time {
	if nowFn != nil {
		return nowFn()
	}
	return time.Now()
}
