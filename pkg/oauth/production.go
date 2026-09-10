package oauth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

// ProductionPaths names durable state files for a multi-instance authorization server.
type ProductionPaths struct {
	StateDir    string
	Clients     string
	Tokens      string
	Replay      string
	SigningKey  string
	SigningKID  string
}

// DefaultProductionPaths returns conventional file paths under stateDir.
func DefaultProductionPaths(stateDir string) ProductionPaths {
	stateDir = strings.TrimSpace(stateDir)
	return ProductionPaths{
		StateDir:   stateDir,
		Clients:    filepath.Join(stateDir, "oauth-clients.json"),
		Tokens:     filepath.Join(stateDir, "oauth-tokens.json"),
		Replay:     filepath.Join(stateDir, "oauth-replay.json"),
		SigningKey: filepath.Join(stateDir, "oauth-signing.pem"),
	}
}

// ProductionStores wires file-backed stores for clustered deployments.
func ProductionStores(paths ProductionPaths) (AuthorizationStore, ClientRegistry, smart.ReplayStore, error) {
	authStore, err := NewFileAuthorizationStore(paths.Tokens)
	if err != nil {
		return nil, nil, nil, err
	}
	clientStore, err := NewFileClientStore(paths.Clients)
	if err != nil {
		return nil, nil, nil, err
	}
	replayStore, err := smart.NewFileReplayStore(paths.Replay)
	if err != nil {
		return nil, nil, nil, err
	}
	return authStore, clientStore, replayStore, nil
}

// LoadSigningKey loads a persistent signing key when the PEM file exists.
func LoadSigningKey(paths ProductionPaths) (*KeySet, error) {
	if strings.TrimSpace(paths.SigningKey) == "" {
		return nil, nil
	}
	if _, err := os.Stat(paths.SigningKey); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return LoadKeySetFromPEM(paths.SigningKey, paths.SigningKID)
}

// NewProductionServer constructs an authorization server with durable state.
func NewProductionServer(cfg Config, paths ProductionPaths) (*Server, error) {
	if strings.TrimSpace(paths.StateDir) == "" {
		return nil, fmt.Errorf("oauth: production state dir required")
	}
	authStore, clientStore, replayStore, err := ProductionStores(paths)
	if err != nil {
		return nil, err
	}
	cfg.AuthorizationStore = authStore
	cfg.Clients = clientStore
	cfg.ReplayStore = replayStore
	if cfg.SigningKey == nil {
		key, err := LoadSigningKey(paths)
		if err != nil {
			return nil, err
		}
		cfg.SigningKey = key
	}
	return NewServer(cfg)
}
