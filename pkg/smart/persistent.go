package smart

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

// FileBackendClientStore persists backend client registrations as a JSON map
// and reloads the file on every lookup, allowing configuration changes to be
// picked up without restarting the process. Protect the file with deployment
// filesystem permissions; production multi-instance deployments should use a
// shared transactional store implementing BackendClientStore.
type FileBackendClientStore struct {
	Path string
	mu   sync.Mutex
}

func NewFileBackendClientStore(path string) (*FileBackendClientStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: backend client store path required", ErrInvalidConfig)
	}
	return &FileBackendClientStore{Path: path}, nil
}

func (s *FileBackendClientStore) LookupBackendClient(clientID string) (BackendClient, error) {
	if s == nil {
		return BackendClient{}, fmt.Errorf("%w: backend client store is nil", ErrClientNotAllowed)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	clients, err := s.load()
	if err != nil {
		return BackendClient{}, err
	}
	client, ok := clients[clientID]
	if !ok {
		return BackendClient{}, fmt.Errorf("%w: %s", ErrClientNotAllowed, clientID)
	}
	return client, nil
}

func (s *FileBackendClientStore) RegisterBackendClient(client BackendClient) error {
	if s == nil {
		return fmt.Errorf("%w: backend client store is nil", ErrInvalidConfig)
	}
	if strings.TrimSpace(client.ClientID) == "" {
		return fmt.Errorf("%w: client id required", ErrInvalidConfig)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	clients, err := s.load()
	if err != nil {
		return err
	}
	clients[client.ClientID] = client
	return s.save(clients)
}

func (s *FileBackendClientStore) load() (map[string]BackendClient, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]BackendClient), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read backend client store: %w", err)
	}
	clients := make(map[string]BackendClient)
	if len(data) == 0 {
		return clients, nil
	}
	if err := json.Unmarshal(data, &clients); err != nil {
		return nil, fmt.Errorf("decode backend client store: %w", err)
	}
	return clients, nil
}

func (s *FileBackendClientStore) save(clients map[string]BackendClient) error {
	data, err := json.MarshalIndent(clients, "", "  ")
	if err != nil {
		return fmt.Errorf("encode backend client store: %w", err)
	}
	return atomicWritePrivateFile(s.Path, data)
}

// FileReplayStore persists used assertion IDs across process restarts. It is
// safe for concurrent use within one process. Use a shared ReplayStore with
// transactional/distributed locking when multiple instances share a path.
type FileReplayStore struct {
	Path string
	Now  func() time.Time
	mu   sync.Mutex
}

func NewFileReplayStore(path string) (*FileReplayStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: replay store path required", ErrInvalidConfig)
	}
	return &FileReplayStore{Path: path}, nil
}

func (s *FileReplayStore) CheckAndStore(jti string, expiresAt time.Time) error {
	if s == nil || strings.TrimSpace(jti) == "" {
		return fmt.Errorf("%w: jti required", ErrReplay)
	}
	nowFn := s.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return err
	}
	for key, expiry := range entries {
		if !expiry.IsZero() && now.After(expiry) {
			delete(entries, key)
		}
	}
	if _, exists := entries[jti]; exists {
		return fmt.Errorf("%w: jti %q", ErrReplay, jti)
	}
	entries[jti] = expiresAt
	if err := s.save(entries); err != nil {
		return err
	}
	return nil
}

func (s *FileReplayStore) load() (map[string]time.Time, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]time.Time), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read replay store: %w", err)
	}
	entries := make(map[string]time.Time)
	if len(data) == 0 {
		return entries, nil
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("decode replay store: %w", err)
	}
	return entries, nil
}

func (s *FileReplayStore) save(entries map[string]time.Time) error {
	data, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("encode replay store: %w", err)
	}
	return atomicWritePrivateFile(s.Path, data)
}

func atomicWritePrivateFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".haistack-state-*")
	if err != nil {
		return fmt.Errorf("create state file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("protect state file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write state file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close state file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace state file: %w", err)
	}
	return nil
}
