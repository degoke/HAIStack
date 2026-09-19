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
	if ok && now.After(entry.ExpiresAt) {
		ok = false
	}
	if ok {
		delete(s.codes, key)
	}
	s.mu.Unlock()
	if !ok {
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
	entry, ok := s.LookupRefreshToken(issuer, token)
	if !ok {
		return RefreshTokenEntry{}, false
	}
	s.mu.Lock()
	delete(s.refreshTokens, key)
	s.mu.Unlock()
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
	s.mu.Unlock()
	if !ok || now.After(entry.ExpiresAt) {
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
	s.mu.Unlock()
	if !ok || now.After(entry.ExpiresAt) {
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
	if ok && (entry.ClientID != clientID || now.After(entry.ExpiresAt)) {
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
	if ok && now.After(entry.ExpiresAt) {
		ok = false
	}
	if ok {
		delete(s.pending, key)
	}
	s.mu.Unlock()
	if !ok {
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

// FileAuthorizationStore persists authorization codes and refresh tokens as JSON.
type FileAuthorizationStore struct {
	Path string
	Now  func() time.Time
	mu   sync.Mutex
}

// NewFileAuthorizationStore constructs a file-backed authorization store.
func NewFileAuthorizationStore(path string) (*FileAuthorizationStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("oauth: authorization store path required")
	}
	return &FileAuthorizationStore{Path: path, Now: time.Now}, nil
}

type fileAuthorizationState struct {
	Codes   map[string]AuthorizationCode    `json:"codes"`
	Refresh map[string]RefreshTokenEntry    `json:"refresh"`
	Pending map[string]PendingAuthorization `json:"pending"`
}

func (s *FileAuthorizationStore) SaveAuthorizationCode(code string, entry AuthorizationCode) error {
	iss, err := RequireBoundIssuer(entry.Issuer)
	if err != nil {
		return err
	}
	entry.Issuer = iss
	key, err := IssuerScopedKey(iss, code)
	if err != nil {
		return err
	}
	return s.update(func(state *fileAuthorizationState) {
		if state.Codes == nil {
			state.Codes = make(map[string]AuthorizationCode)
		}
		state.Codes[key] = entry
	})
}

func (s *FileAuthorizationStore) ConsumeAuthorizationCode(issuer, code string) (AuthorizationCode, bool) {
	key, err := IssuerScopedKey(issuer, code)
	if err != nil {
		return AuthorizationCode{}, false
	}
	now := s.now()
	var entry AuthorizationCode
	var ok bool
	err = s.update(func(state *fileAuthorizationState) {
		entry, ok = state.Codes[key]
		if ok && now.After(entry.ExpiresAt) {
			ok = false
		}
		if ok {
			delete(state.Codes, key)
		}
	})
	if err != nil || !ok {
		return AuthorizationCode{}, false
	}
	return entry, true
}

func (s *FileAuthorizationStore) SaveRefreshToken(token string, entry RefreshTokenEntry) error {
	iss, err := RequireBoundIssuer(entry.Issuer)
	if err != nil {
		return err
	}
	entry.Issuer = iss
	key, err := IssuerScopedKey(iss, token)
	if err != nil {
		return err
	}
	return s.update(func(state *fileAuthorizationState) {
		if state.Refresh == nil {
			state.Refresh = make(map[string]RefreshTokenEntry)
		}
		state.Refresh[key] = entry
	})
}

func (s *FileAuthorizationStore) ConsumeRefreshToken(issuer, token string) (RefreshTokenEntry, bool) {
	key, err := IssuerScopedKey(issuer, token)
	if err != nil {
		return RefreshTokenEntry{}, false
	}
	entry, ok := s.LookupRefreshToken(issuer, token)
	if !ok {
		return RefreshTokenEntry{}, false
	}
	err = s.update(func(state *fileAuthorizationState) {
		delete(state.Refresh, key)
	})
	if err != nil {
		return RefreshTokenEntry{}, false
	}
	return entry, true
}

func (s *FileAuthorizationStore) LookupRefreshToken(issuer, token string) (RefreshTokenEntry, bool) {
	key, err := IssuerScopedKey(issuer, token)
	if err != nil {
		return RefreshTokenEntry{}, false
	}
	now := s.now()
	var entry RefreshTokenEntry
	var ok bool
	err = s.update(func(state *fileAuthorizationState) {
		entry, ok = state.Refresh[key]
	})
	if err != nil || !ok || now.After(entry.ExpiresAt) {
		return RefreshTokenEntry{}, false
	}
	return entry, true
}

func (s *FileAuthorizationStore) SavePendingAuthorization(id string, entry PendingAuthorization) error {
	iss, err := RequireBoundIssuer(entry.Issuer)
	if err != nil {
		return err
	}
	entry.Issuer = iss
	key, err := IssuerScopedKey(iss, id)
	if err != nil {
		return err
	}
	return s.update(func(state *fileAuthorizationState) {
		if state.Pending == nil {
			state.Pending = make(map[string]PendingAuthorization)
		}
		state.Pending[key] = entry
	})
}

func (s *FileAuthorizationStore) GetPendingAuthorization(issuer, id string) (PendingAuthorization, bool) {
	key, err := IssuerScopedKey(issuer, id)
	if err != nil {
		return PendingAuthorization{}, false
	}
	now := s.now()
	var entry PendingAuthorization
	var ok bool
	err = s.update(func(state *fileAuthorizationState) {
		entry, ok = state.Pending[key]
	})
	if err != nil || !ok || now.After(entry.ExpiresAt) {
		return PendingAuthorization{}, false
	}
	return entry, true
}

func (s *FileAuthorizationStore) ConsumePendingAuthorization(issuer, id string) (PendingAuthorization, bool) {
	key, err := IssuerScopedKey(issuer, id)
	if err != nil {
		return PendingAuthorization{}, false
	}
	now := s.now()
	var entry PendingAuthorization
	var ok bool
	err = s.update(func(state *fileAuthorizationState) {
		entry, ok = state.Pending[key]
		if ok && now.After(entry.ExpiresAt) {
			ok = false
		}
		if ok {
			delete(state.Pending, key)
		}
	})
	if err != nil || !ok {
		return PendingAuthorization{}, false
	}
	return entry, true
}

func (s *FileAuthorizationStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *FileAuthorizationStore) PurgeExpiredPendingAuthorizations() int {
	now := s.now()
	var n int
	_ = s.update(func(state *fileAuthorizationState) {
		for id, entry := range state.Pending {
			if now.After(entry.ExpiresAt) {
				delete(state.Pending, id)
				n++
			}
		}
	})
	return n
}

func (s *FileAuthorizationStore) DeleteRefreshTokenForClient(issuer, token, clientID string) bool {
	key, err := IssuerScopedKey(issuer, token)
	if err != nil {
		return false
	}
	now := s.now()
	var ok bool
	err = s.update(func(state *fileAuthorizationState) {
		entry, found := state.Refresh[key]
		if !found || entry.ClientID != clientID || now.After(entry.ExpiresAt) {
			return
		}
		delete(state.Refresh, key)
		ok = true
	})
	return err == nil && ok
}

func (s *FileAuthorizationStore) update(fn func(*fileAuthorizationState)) error {
	if s == nil {
		return fmt.Errorf("oauth: authorization store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.load()
	if err != nil {
		return err
	}
	purgeExpiredAuthorizationState(&state, s.now())
	fn(&state)
	return s.save(state)
}

func purgeExpiredAuthorizationState(state *fileAuthorizationState, now time.Time) {
	for code, entry := range state.Codes {
		if now.After(entry.ExpiresAt) {
			delete(state.Codes, code)
		}
	}
	for token, entry := range state.Refresh {
		if now.After(entry.ExpiresAt) {
			delete(state.Refresh, token)
		}
	}
	for id, entry := range state.Pending {
		if now.After(entry.ExpiresAt) {
			delete(state.Pending, id)
		}
	}
}

func (s *FileAuthorizationStore) load() (fileAuthorizationState, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return fileAuthorizationState{
			Codes:   make(map[string]AuthorizationCode),
			Refresh: make(map[string]RefreshTokenEntry),
			Pending: make(map[string]PendingAuthorization),
		}, nil
	}
	if err != nil {
		return fileAuthorizationState{}, fmt.Errorf("read authorization store: %w", err)
	}
	if len(data) == 0 {
		return fileAuthorizationState{
			Codes:   make(map[string]AuthorizationCode),
			Refresh: make(map[string]RefreshTokenEntry),
			Pending: make(map[string]PendingAuthorization),
		}, nil
	}
	var state fileAuthorizationState
	if err := json.Unmarshal(data, &state); err != nil {
		return fileAuthorizationState{}, fmt.Errorf("decode authorization store: %w", err)
	}
	if state.Codes == nil {
		state.Codes = make(map[string]AuthorizationCode)
	}
	if state.Refresh == nil {
		state.Refresh = make(map[string]RefreshTokenEntry)
	}
	if state.Pending == nil {
		state.Pending = make(map[string]PendingAuthorization)
	}
	return state, nil
}

func (s *FileAuthorizationStore) save(state fileAuthorizationState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode authorization store: %w", err)
	}
	return atomicWritePrivateFile(s.Path, data)
}
