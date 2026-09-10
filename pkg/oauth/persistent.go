package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AuthorizationStore persists authorization codes, refresh tokens, and pending
// authorization sessions. File-backed implementations support multi-instance AS
// deployments that share a filesystem.
type AuthorizationStore interface {
	SaveAuthorizationCode(code string, entry AuthorizationCode) error
	ConsumeAuthorizationCode(code string) (AuthorizationCode, bool)
	SaveRefreshToken(token string, entry RefreshTokenEntry) error
	ConsumeRefreshToken(token string) (RefreshTokenEntry, bool)
	SavePendingAuthorization(id string, entry PendingAuthorization) error
	GetPendingAuthorization(id string) (PendingAuthorization, bool)
	ConsumePendingAuthorization(id string) (PendingAuthorization, bool)
}

// PendingAuthorization stores an in-progress authorize/consent/launch session.
type PendingAuthorization struct {
	Request   AuthorizationRequest `json:"request"`
	ExpiresAt time.Time            `json:"expiresAt"`
}

// AuthorizationCode is a short-lived authorization code entry.
type AuthorizationCode struct {
	ClientID    string    `json:"clientId"`
	RedirectURI string    `json:"redirectUri"`
	Scope       string    `json:"scope"`
	Patient     string    `json:"patient,omitempty"`
	Challenge   string    `json:"challenge,omitempty"`
	Method      string    `json:"method,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// RefreshTokenEntry stores refresh-token metadata.
type RefreshTokenEntry struct {
	ClientID  string    `json:"clientId"`
	Scope     string    `json:"scope"`
	Patient   string    `json:"patient,omitempty"`
	Subject   string    `json:"subject"`
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
	s.mu.Lock()
	s.codes[code] = entry
	s.mu.Unlock()
	return nil
}

func (s *MemoryAuthorizationStore) ConsumeAuthorizationCode(code string) (AuthorizationCode, bool) {
	now := memoryStoreNow(s)
	s.mu.Lock()
	entry, ok := s.codes[code]
	if ok {
		delete(s.codes, code)
	}
	s.mu.Unlock()
	if !ok || now.After(entry.ExpiresAt) {
		return AuthorizationCode{}, false
	}
	return entry, true
}

func (s *MemoryAuthorizationStore) SaveRefreshToken(token string, entry RefreshTokenEntry) error {
	s.mu.Lock()
	s.refreshTokens[token] = entry
	s.mu.Unlock()
	return nil
}

func (s *MemoryAuthorizationStore) ConsumeRefreshToken(token string) (RefreshTokenEntry, bool) {
	now := memoryStoreNow(s)
	s.mu.Lock()
	entry, ok := s.refreshTokens[token]
	if ok {
		delete(s.refreshTokens, token)
	}
	s.mu.Unlock()
	if !ok || now.After(entry.ExpiresAt) {
		return RefreshTokenEntry{}, false
	}
	return entry, true
}

func (s *MemoryAuthorizationStore) SavePendingAuthorization(id string, entry PendingAuthorization) error {
	s.mu.Lock()
	s.pending[id] = entry
	s.mu.Unlock()
	return nil
}

func (s *MemoryAuthorizationStore) GetPendingAuthorization(id string) (PendingAuthorization, bool) {
	now := memoryStoreNow(s)
	s.mu.Lock()
	entry, ok := s.pending[id]
	s.mu.Unlock()
	if !ok || now.After(entry.ExpiresAt) {
		return PendingAuthorization{}, false
	}
	return entry, true
}

func (s *MemoryAuthorizationStore) ConsumePendingAuthorization(id string) (PendingAuthorization, bool) {
	now := memoryStoreNow(s)
	s.mu.Lock()
	entry, ok := s.pending[id]
	if ok {
		delete(s.pending, id)
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
	Codes    map[string]AuthorizationCode   `json:"codes"`
	Refresh  map[string]RefreshTokenEntry   `json:"refresh"`
	Pending  map[string]PendingAuthorization `json:"pending"`
}

func (s *FileAuthorizationStore) SaveAuthorizationCode(code string, entry AuthorizationCode) error {
	return s.update(func(state *fileAuthorizationState) {
		if state.Codes == nil {
			state.Codes = make(map[string]AuthorizationCode)
		}
		state.Codes[code] = entry
	})
}

func (s *FileAuthorizationStore) ConsumeAuthorizationCode(code string) (AuthorizationCode, bool) {
	now := s.now()
	var entry AuthorizationCode
	var ok bool
	err := s.update(func(state *fileAuthorizationState) {
		entry, ok = state.Codes[code]
		if ok {
			delete(state.Codes, code)
		}
	})
	if err != nil || !ok || now.After(entry.ExpiresAt) {
		return AuthorizationCode{}, false
	}
	return entry, true
}

func (s *FileAuthorizationStore) SaveRefreshToken(token string, entry RefreshTokenEntry) error {
	return s.update(func(state *fileAuthorizationState) {
		if state.Refresh == nil {
			state.Refresh = make(map[string]RefreshTokenEntry)
		}
		state.Refresh[token] = entry
	})
}

func (s *FileAuthorizationStore) ConsumeRefreshToken(token string) (RefreshTokenEntry, bool) {
	now := s.now()
	var entry RefreshTokenEntry
	var ok bool
	err := s.update(func(state *fileAuthorizationState) {
		entry, ok = state.Refresh[token]
		if ok {
			delete(state.Refresh, token)
		}
	})
	if err != nil || !ok || now.After(entry.ExpiresAt) {
		return RefreshTokenEntry{}, false
	}
	return entry, true
}

func (s *FileAuthorizationStore) SavePendingAuthorization(id string, entry PendingAuthorization) error {
	return s.update(func(state *fileAuthorizationState) {
		if state.Pending == nil {
			state.Pending = make(map[string]PendingAuthorization)
		}
		state.Pending[id] = entry
	})
}

func (s *FileAuthorizationStore) GetPendingAuthorization(id string) (PendingAuthorization, bool) {
	now := s.now()
	var entry PendingAuthorization
	var ok bool
	err := s.update(func(state *fileAuthorizationState) {
		entry, ok = state.Pending[id]
	})
	if err != nil || !ok || now.After(entry.ExpiresAt) {
		return PendingAuthorization{}, false
	}
	return entry, true
}

func (s *FileAuthorizationStore) ConsumePendingAuthorization(id string) (PendingAuthorization, bool) {
	now := s.now()
	var entry PendingAuthorization
	var ok bool
	err := s.update(func(state *fileAuthorizationState) {
		entry, ok = state.Pending[id]
		if ok {
			delete(state.Pending, id)
		}
	})
	if err != nil || !ok || now.After(entry.ExpiresAt) {
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
	fn(&state)
	return s.save(state)
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
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("create authorization store dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode authorization store: %w", err)
	}
	return os.WriteFile(s.Path, data, 0o600)
}
