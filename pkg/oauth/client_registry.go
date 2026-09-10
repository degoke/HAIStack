package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// ClientRegistry stores registered OAuth clients.
type ClientRegistry interface {
	Get(clientID string) (Client, bool)
	Register(client Client) error
}

// FileClientStore persists OAuth clients as JSON.
type FileClientStore struct {
	Path string
	mu   sync.Mutex
}

// NewFileClientStore constructs a file-backed client registry.
func NewFileClientStore(path string) (*FileClientStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("oauth: client store path required")
	}
	return &FileClientStore{Path: path}, nil
}

func (s *FileClientStore) Get(clientID string) (Client, bool) {
	if s == nil {
		return Client{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	clients, err := s.load()
	if err != nil {
		return Client{}, false
	}
	client, ok := clients[clientID]
	return client, ok
}

func (s *FileClientStore) Register(client Client) error {
	if s == nil {
		return fmt.Errorf("oauth: client store is nil")
	}
	if err := prepareClientSecret(&client); err != nil {
		return err
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

func (s *FileClientStore) load() (map[string]Client, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]Client), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read client store: %w", err)
	}
	clients := make(map[string]Client)
	if len(data) == 0 {
		return clients, nil
	}
	if err := json.Unmarshal(data, &clients); err != nil {
		return nil, fmt.Errorf("decode client store: %w", err)
	}
	return clients, nil
}

func (s *FileClientStore) save(clients map[string]Client) error {
	data, err := json.MarshalIndent(clients, "", "  ")
	if err != nil {
		return fmt.Errorf("encode client store: %w", err)
	}
	return atomicWritePrivateFile(s.Path, data)
}
