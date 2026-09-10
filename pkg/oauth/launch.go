package oauth

import (
	"fmt"
	"sync"
	"time"
)

const defaultLaunchTokenTTL = 5 * time.Minute

// LaunchContextRecord holds EHR launch context bound to a one-time launch token.
type LaunchContextRecord struct {
	PatientID   string
	EncounterID string
	UserID      string
	TenantHint  string
	ExpiresAt   time.Time
}

// LaunchStore persists single-use SMART launch tokens issued by an EHR context.
type LaunchStore interface {
	Issue(record LaunchContextRecord) (string, error)
	Consume(token string) (LaunchContextRecord, error)
}

// MemoryLaunchStore stores launch tokens in memory.
type MemoryLaunchStore struct {
	mu     sync.Mutex
	tokens map[string]launchEntry
	Now    func() time.Time
}

type launchEntry struct {
	LaunchContextRecord
	Used bool
}

// NewMemoryLaunchStore returns an in-memory launch token store.
func NewMemoryLaunchStore() *MemoryLaunchStore {
	return &MemoryLaunchStore{tokens: make(map[string]launchEntry)}
}

func (s *MemoryLaunchStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *MemoryLaunchStore) Issue(record LaunchContextRecord) (string, error) {
	if s == nil {
		return "", fmt.Errorf("%w: launch store is nil", ErrInvalidConfig)
	}
	token, err := NewRandomToken()
	if err != nil {
		return "", err
	}
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = s.now().Add(defaultLaunchTokenTTL)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokens == nil {
		s.tokens = make(map[string]launchEntry)
	}
	s.purgeLocked()
	s.tokens[token] = launchEntry{LaunchContextRecord: record}
	return token, nil
}

func (s *MemoryLaunchStore) Consume(token string) (LaunchContextRecord, error) {
	if s == nil {
		return LaunchContextRecord{}, fmt.Errorf("%w: launch store is nil", ErrInvalidConfig)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	entry, ok := s.tokens[token]
	if !ok || entry.Used {
		return LaunchContextRecord{}, fmt.Errorf("%w: unknown or used launch token", ErrInvalidGrant)
	}
	if s.now().After(entry.ExpiresAt) {
		delete(s.tokens, token)
		return LaunchContextRecord{}, fmt.Errorf("%w: launch token expired", ErrInvalidGrant)
	}
	entry.Used = true
	delete(s.tokens, token)
	return entry.LaunchContextRecord, nil
}

func (s *MemoryLaunchStore) purgeLocked() {
	now := s.now()
	for key, entry := range s.tokens {
		if entry.Used || now.After(entry.ExpiresAt) {
			delete(s.tokens, key)
		}
	}
}
