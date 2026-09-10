package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	defaultAuthCodeTTL = 2 * time.Minute
	authCodeBytes      = 32
)

// AuthCode holds a single-use authorization code with optional PKCE material.
type AuthCode struct {
	Code                string
	ClientID            string
	RedirectURI         string
	Scope               string
	CodeChallenge       string
	CodeChallengeMethod string
	State               string
	Patient             string
	Encounter           string
	User                string
	TenantHint          string
	ExpiresAt           time.Time
	Used                bool
}

// CodeStore persists authorization codes in memory for v1.
type CodeStore struct {
	mu    sync.Mutex
	codes map[string]*AuthCode
	Now   func() time.Time
}

// NewCodeStore returns an in-memory authorization code store.
func NewCodeStore() *CodeStore {
	return &CodeStore{codes: make(map[string]*AuthCode)}
}

func (s *CodeStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Issue creates and stores a new authorization code.
func (s *CodeStore) Issue(entry AuthCode) (string, error) {
	if s == nil {
		return "", fmt.Errorf("%w: code store is nil", ErrInvalidConfig)
	}
	code, err := randomURLSafe(authCodeBytes)
	if err != nil {
		return "", err
	}
	entry.Code = code
	if entry.ExpiresAt.IsZero() {
		entry.ExpiresAt = s.now().Add(defaultAuthCodeTTL)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.codes == nil {
		s.codes = make(map[string]*AuthCode)
	}
	s.purgeLocked()
	copy := entry
	s.codes[code] = &copy
	return code, nil
}

// Exchange validates and consumes an authorization code.
func (s *CodeStore) Exchange(code, clientID, redirectURI, codeVerifier string) (*AuthCode, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: code store is nil", ErrInvalidConfig)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	entry, ok := s.codes[code]
	if !ok || entry.Used {
		return nil, fmt.Errorf("%w: unknown or used code", ErrInvalidGrant)
	}
	if s.now().After(entry.ExpiresAt) {
		delete(s.codes, code)
		return nil, fmt.Errorf("%w: code expired", ErrInvalidGrant)
	}
	if entry.ClientID != clientID {
		return nil, fmt.Errorf("%w: client mismatch", ErrInvalidGrant)
	}
	if entry.RedirectURI != redirectURI {
		return nil, fmt.Errorf("%w: redirect_uri mismatch", ErrInvalidGrant)
	}
	if entry.CodeChallenge != "" {
		if err := verifyPKCE(entry.CodeChallenge, entry.CodeChallengeMethod, codeVerifier); err != nil {
			return nil, err
		}
	}
	entry.Used = true
	delete(s.codes, code)
	copy := *entry
	return &copy, nil
}

func (s *CodeStore) purgeLocked() {
	now := s.now()
	for key, entry := range s.codes {
		if entry == nil || now.After(entry.ExpiresAt) || entry.Used {
			delete(s.codes, key)
		}
	}
}

func verifyPKCE(challenge, method, verifier string) error {
	verifier = strings.TrimSpace(verifier)
	if verifier == "" {
		return fmt.Errorf("%w: code_verifier required", ErrInvalidGrant)
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "S256"
	}
	if method == "PLAIN" {
		return fmt.Errorf("%w: plain code_challenge_method is not allowed", ErrInvalidGrant)
	}
	if method != "S256" {
		return fmt.Errorf("%w: unsupported code_challenge_method %q", ErrInvalidGrant, method)
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	if computed != challenge {
		return fmt.Errorf("%w: pkce verification failed", ErrInvalidGrant)
	}
	return nil
}

func randomURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// NewRandomToken generates a URL-safe opaque token.
func NewRandomToken() (string, error) {
	return randomURLSafe(authCodeBytes)
}

// VerifyPKCE validates a PKCE code_verifier against the stored challenge.
func VerifyPKCE(challenge, method, verifier string) error {
	return verifyPKCE(challenge, method, verifier)
}
