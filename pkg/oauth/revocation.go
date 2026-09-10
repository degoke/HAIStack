package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

// FileTokenRevocationStore persists revoked JTIs as JSON.
type FileTokenRevocationStore struct {
	Path string
	Now  func() time.Time
	mu   sync.Mutex
}

// NewFileTokenRevocationStore constructs a file-backed revocation store.
func NewFileTokenRevocationStore(path string) (*FileTokenRevocationStore, error) {
	if path == "" {
		return nil, fmt.Errorf("oauth: revocation store path required")
	}
	return &FileTokenRevocationStore{Path: path, Now: time.Now}, nil
}

func (s *FileTokenRevocationStore) Revoke(jti string, expiresAt time.Time) error {
	return s.update(func(entries map[string]time.Time) {
		entries[jti] = expiresAt
	})
}

func (s *FileTokenRevocationStore) IsRevoked(jti string) bool {
	now := revocationNow(s.Now)
	var revoked bool
	_ = s.update(func(entries map[string]time.Time) {
		expiry, ok := entries[jti]
		if ok && !expiry.IsZero() && now.After(expiry) {
			delete(entries, jti)
			ok = false
		}
		revoked = ok
	})
	return revoked
}

func (s *FileTokenRevocationStore) update(fn func(map[string]time.Time)) error {
	if s == nil {
		return fmt.Errorf("oauth: revocation store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return err
	}
	purgeExpiredRevocations(entries, revocationNow(s.Now))
	fn(entries)
	return s.save(entries)
}

func (s *FileTokenRevocationStore) load() (map[string]time.Time, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]time.Time), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read revocation store: %w", err)
	}
	entries := make(map[string]time.Time)
	if len(data) == 0 {
		return entries, nil
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("decode revocation store: %w", err)
	}
	return entries, nil
}

func (s *FileTokenRevocationStore) save(entries map[string]time.Time) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode revocation store: %w", err)
	}
	return atomicWritePrivateFile(s.Path, data)
}

func purgeExpiredRevocations(entries map[string]time.Time, now time.Time) {
	for jti, expiry := range entries {
		if !expiry.IsZero() && now.After(expiry) {
			delete(entries, jti)
		}
	}
}

func revocationNow(nowFn func() time.Time) time.Time {
	if nowFn != nil {
		return nowFn()
	}
	return time.Now()
}
