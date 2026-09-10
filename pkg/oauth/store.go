package oauth

import (
	"sync"
	"time"
)

type authCode struct {
	ClientID    string
	RedirectURI string
	Scope       string
	Patient     string
	Challenge   string
	Method      string
	ExpiresAt   time.Time
}

type refreshToken struct {
	ClientID  string
	Scope     string
	Patient   string
	Subject   string
	ExpiresAt time.Time
}

// TokenStore holds short-lived authorization codes and refresh tokens.
type TokenStore struct {
	mu            sync.Mutex
	codes         map[string]authCode
	refreshTokens map[string]refreshToken
}

// NewTokenStore constructs an in-memory token store.
func NewTokenStore() *TokenStore {
	return &TokenStore{
		codes:         make(map[string]authCode),
		refreshTokens: make(map[string]refreshToken),
	}
}

func (s *TokenStore) saveCode(code string, entry authCode) {
	s.mu.Lock()
	s.codes[code] = entry
	s.mu.Unlock()
}

func (s *TokenStore) consumeCode(code string) (authCode, bool) {
	s.mu.Lock()
	entry, ok := s.codes[code]
	if ok {
		delete(s.codes, code)
	}
	s.mu.Unlock()
	if !ok || time.Now().After(entry.ExpiresAt) {
		return authCode{}, false
	}
	return entry, true
}

func (s *TokenStore) saveRefresh(token string, entry refreshToken) {
	s.mu.Lock()
	s.refreshTokens[token] = entry
	s.mu.Unlock()
}

func (s *TokenStore) consumeRefresh(token string) (refreshToken, bool) {
	s.mu.Lock()
	entry, ok := s.refreshTokens[token]
	if ok {
		delete(s.refreshTokens, token)
	}
	s.mu.Unlock()
	if !ok || time.Now().After(entry.ExpiresAt) {
		return refreshToken{}, false
	}
	return entry, true
}
