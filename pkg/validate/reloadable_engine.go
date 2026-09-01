package validate

import (
	"context"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// ReloadableEngine is a thread-safe validate.Engine whose snapshot-backed state
// can be refreshed after registry catalog changes.
type ReloadableEngine struct {
	mu     sync.RWMutex
	engine Engine
	cfg    Config
}

// NewReloadableEngine constructs an engine and retains the config for reload.
func NewReloadableEngine(cfg Config) (*ReloadableEngine, error) {
	engine, err := NewEngine(cfg)
	if err != nil {
		return nil, err
	}
	return &ReloadableEngine{engine: engine, cfg: cfg}, nil
}

// Validate implements Engine.
func (e *ReloadableEngine) Validate(ctx context.Context, res *types.ResourceEnvelope, opts ValidateOptions) (*ValidationResult, error) {
	e.mu.RLock()
	engine := e.engine
	e.mu.RUnlock()
	return engine.Validate(ctx, res, opts)
}

// Reload rebuilds the inner engine from an updated config.
func (e *ReloadableEngine) Reload(cfg Config) error {
	engine, err := NewEngine(cfg)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.engine = engine
	e.cfg = cfg
	e.mu.Unlock()
	return nil
}

// Config returns the last reload configuration.
func (e *ReloadableEngine) Config() Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}
