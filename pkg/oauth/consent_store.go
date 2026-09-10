package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/url"
	"sync"
	"time"
)

type consentSession struct {
	Issuer    string
	Params    url.Values
	CSRF      string
	Subject   string
	ExpiresAt time.Time
}

// ConsentSessionStore persists short-lived OAuth authorize consent state.
type ConsentSessionStore interface {
	Create(issuer string, params url.Values, subject string) (sessionID, csrf string, err error)
	Consume(issuer, sessionID, csrf string) (url.Values, string, error)
	// PurgeExpired removes expired sessions and returns the number deleted.
	PurgeExpired(ctx context.Context) (removed int64, err error)
}

type memoryConsentSessionStore struct {
	mu       sync.Mutex
	sessions map[string]consentSession
	nowFn    func() time.Time
}

// NewMemoryConsentSessionStore returns an in-memory consent session store.
func NewMemoryConsentSessionStore(nowFn func() time.Time) *memoryConsentSessionStore {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &memoryConsentSessionStore{
		sessions: make(map[string]consentSession),
		nowFn:    nowFn,
	}
}

func (s *memoryConsentSessionStore) Create(issuer string, params url.Values, subject string) (sessionID, csrf string, err error) {
	sessionID, err = randomURLSafeToken(24)
	if err != nil {
		return "", "", err
	}
	csrf, err = randomURLSafeToken(32)
	if err != nil {
		return "", "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	s.sessions[sessionID] = consentSession{
		Issuer:    issuer,
		Params:    cloneValues(params),
		CSRF:      csrf,
		Subject:   subject,
		ExpiresAt: s.nowFn().Add(defaultConsentSessionTTL),
	}
	return sessionID, csrf, nil
}

func (s *memoryConsentSessionStore) Consume(issuer, sessionID, csrf string) (url.Values, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, "", ErrInvalidRequest
	}
	delete(s.sessions, sessionID)
	if s.nowFn().After(session.ExpiresAt) {
		return nil, "", ErrInvalidRequest
	}
	if session.Issuer != issuer {
		return nil, "", ErrInvalidRequest
	}
	if !tokenEqual(csrf, session.CSRF) {
		return nil, "", ErrInvalidRequest
	}
	return cloneValues(session.Params), session.Subject, nil
}

func (s *memoryConsentSessionStore) purgeLocked() {
	now := s.nowFn()
	for id, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
}

func (s *memoryConsentSessionStore) PurgeExpired(ctx context.Context) (int64, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.nowFn()
	var removed int64
	for id, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, id)
			removed++
		}
	}
	return removed, nil
}

func randomURLSafeToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
