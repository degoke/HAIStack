package oauth

import (
	"sync"
	"time"
)

// AuthorizationStore persists authorization codes, refresh tokens, and pending
// authorization sessions. Production deployments use pkg/oauth/store (SQLite or Postgres).
type AuthorizationStore interface {
	SaveAuthorizationCode(code string, entry AuthorizationCode) error
	ConsumeAuthorizationCode(issuer, code string) (AuthorizationCode, bool)
	SaveRefreshToken(token string, entry RefreshTokenEntry) error
	ConsumeRefreshToken(issuer, token string) (RefreshTokenEntry, bool)
	LookupRefreshToken(issuer, token string) (RefreshTokenEntry, bool)
	SavePendingAuthorization(id string, entry PendingAuthorization) error
	GetPendingAuthorization(issuer, id string) (PendingAuthorization, bool)
	ConsumePendingAuthorization(issuer, id string) (PendingAuthorization, bool)
	DeleteRefreshTokenForClient(issuer, token, clientID string) bool
	// PurgeExpiredPendingAuthorizations removes expired consent sessions.
	PurgeExpiredPendingAuthorizations() int
}

// PendingAuthorization stores an in-progress authorize/consent/launch session.
type PendingAuthorization struct {
	Issuer    string               `json:"issuer,omitempty"`
	Request   AuthorizationRequest `json:"request"`
	Subject   string               `json:"subject,omitempty"`
	FHIRUser  string               `json:"fhirUser,omitempty"`
	CSRFToken string               `json:"csrfToken,omitempty"`
	ExpiresAt time.Time            `json:"expiresAt"`
	Exp       int64                `json:"exp,omitempty"`
}

// AuthorizationCode is a short-lived authorization code entry.
type AuthorizationCode struct {
	Issuer      string    `json:"issuer,omitempty"`
	ClientID    string    `json:"clientId"`
	RedirectURI string    `json:"redirectUri"`
	Scope       string    `json:"scope"`
	Patient     string    `json:"patient,omitempty"`
	Encounter   string    `json:"encounter,omitempty"`
	Subject     string    `json:"subject,omitempty"`
	FHIRUser    string    `json:"fhirUser,omitempty"`
	Challenge   string    `json:"challenge,omitempty"`
	Method      string    `json:"method,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Exp         int64     `json:"exp,omitempty"`
}

// RefreshTokenEntry stores refresh-token metadata.
type RefreshTokenEntry struct {
	Issuer    string    `json:"issuer,omitempty"`
	ClientID  string    `json:"clientId"`
	Scope     string    `json:"scope"`
	Patient   string    `json:"patient,omitempty"`
	Encounter string    `json:"encounter,omitempty"`
	Subject   string    `json:"subject"`
	FHIRUser  string    `json:"fhirUser,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
	Exp       int64     `json:"exp,omitempty"`
}

// MemoryAuthorizationStore is a process-local AuthorizationStore.
type MemoryAuthorizationStore struct {
	mu            sync.Mutex
	codes         map[string]AuthorizationCode
	refreshTokens map[string]RefreshTokenEntry
	pending       map[string]PendingAuthorization
	Now           func() time.Time
}

// NewMemoryAuthorizationStore constructs an in-memory authorization store.
func NewMemoryAuthorizationStore() *MemoryAuthorizationStore {
	return &MemoryAuthorizationStore{
		codes:         make(map[string]AuthorizationCode),
		refreshTokens: make(map[string]RefreshTokenEntry),
		pending:       make(map[string]PendingAuthorization),
		Now:           time.Now,
	}
}

func (s *MemoryAuthorizationStore) SaveAuthorizationCode(code string, entry AuthorizationCode) error {
	iss, err := RequireBoundIssuer(entry.Issuer)
	if err != nil {
		return err
	}
	entry.Issuer = iss
	entry.Exp = entry.ExpiresAt.UnixMilli()
	key, err := IssuerScopedKey(iss, code)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.codes[key] = entry
	s.mu.Unlock()
	return nil
}

func (s *MemoryAuthorizationStore) ConsumeAuthorizationCode(issuer, code string) (AuthorizationCode, bool) {
	key, err := IssuerScopedKey(issuer, code)
	if err != nil {
		return AuthorizationCode{}, false
	}
	now := memoryStoreNow(s)
	s.mu.Lock()
	entry, ok := s.codes[key]
	if ok {
		delete(s.codes, key)
	}
	s.mu.Unlock()
	if !ok || now.After(entry.ExpiresAt) {
		return AuthorizationCode{}, false
	}
	return entry, true
}

func (s *MemoryAuthorizationStore) SaveRefreshToken(token string, entry RefreshTokenEntry) error {
	iss, err := RequireBoundIssuer(entry.Issuer)
	if err != nil {
		return err
	}
	entry.Issuer = iss
	entry.Exp = entry.ExpiresAt.UnixMilli()
	key, err := IssuerScopedKey(iss, token)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.refreshTokens[key] = entry
	s.mu.Unlock()
	return nil
}

func (s *MemoryAuthorizationStore) ConsumeRefreshToken(issuer, token string) (RefreshTokenEntry, bool) {
	key, err := IssuerScopedKey(issuer, token)
	if err != nil {
		return RefreshTokenEntry{}, false
	}
	now := memoryStoreNow(s)
	s.mu.Lock()
	entry, ok := s.refreshTokens[key]
	if ok {
		delete(s.refreshTokens, key)
	}
	s.mu.Unlock()
	if !ok || now.After(entry.ExpiresAt) {
		return RefreshTokenEntry{}, false
	}
	return entry, true
}

func (s *MemoryAuthorizationStore) LookupRefreshToken(issuer, token string) (RefreshTokenEntry, bool) {
	key, err := IssuerScopedKey(issuer, token)
	if err != nil {
		return RefreshTokenEntry{}, false
	}
	now := memoryStoreNow(s)
	s.mu.Lock()
	entry, ok := s.refreshTokens[key]
	if ok && now.After(entry.ExpiresAt) {
		delete(s.refreshTokens, key)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		return RefreshTokenEntry{}, false
	}
	return entry, true
}

func (s *MemoryAuthorizationStore) SavePendingAuthorization(id string, entry PendingAuthorization) error {
	iss, err := RequireBoundIssuer(entry.Issuer)
	if err != nil {
		return err
	}
	entry.Issuer = iss
	entry.Exp = entry.ExpiresAt.UnixMilli()
	key, err := IssuerScopedKey(iss, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.pending[key] = entry
	s.mu.Unlock()
	return nil
}

func (s *MemoryAuthorizationStore) GetPendingAuthorization(issuer, id string) (PendingAuthorization, bool) {
	key, err := IssuerScopedKey(issuer, id)
	if err != nil {
		return PendingAuthorization{}, false
	}
	now := memoryStoreNow(s)
	s.mu.Lock()
	entry, ok := s.pending[key]
	if ok && now.After(entry.ExpiresAt) {
		delete(s.pending, key)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		return PendingAuthorization{}, false
	}
	return entry, true
}

func (s *MemoryAuthorizationStore) DeleteRefreshTokenForClient(issuer, token, clientID string) bool {
	key, err := IssuerScopedKey(issuer, token)
	if err != nil {
		return false
	}
	now := memoryStoreNow(s)
	s.mu.Lock()
	entry, ok := s.refreshTokens[key]
	if ok && now.After(entry.ExpiresAt) {
		delete(s.refreshTokens, key)
		ok = false
	}
	if ok && entry.ClientID != clientID {
		ok = false
	}
	if ok {
		delete(s.refreshTokens, key)
	}
	s.mu.Unlock()
	return ok
}

func (s *MemoryAuthorizationStore) PurgeExpiredPendingAuthorizations() int {
	now := memoryStoreNow(s)
	s.mu.Lock()
	n := 0
	for id, entry := range s.pending {
		if now.After(entry.ExpiresAt) {
			delete(s.pending, id)
			n++
		}
	}
	s.mu.Unlock()
	return n
}

func (s *MemoryAuthorizationStore) ConsumePendingAuthorization(issuer, id string) (PendingAuthorization, bool) {
	key, err := IssuerScopedKey(issuer, id)
	if err != nil {
		return PendingAuthorization{}, false
	}
	now := memoryStoreNow(s)
	s.mu.Lock()
	entry, ok := s.pending[key]
	if ok {
		delete(s.pending, key)
	}
	s.mu.Unlock()
	if !ok || now.After(entry.ExpiresAt) {
		return PendingAuthorization{}, false
	}
	return entry, true
}

func memoryStoreNow(s *MemoryAuthorizationStore) time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
