package sync

import "context"

// Engine is the high-level sync entrypoint for device nodes.
type Engine struct {
	Config Config
}

// NewEngine constructs a sync engine from config.
func NewEngine(cfg Config) *Engine {
	return &Engine{Config: cfg.normalized()}
}

// Push proposes pending local outbox events to the hub.
func (e *Engine) Push(ctx context.Context) (*PushResultSummary, error) {
	return (&Pusher{Config: e.Config}).Push(ctx)
}

// Pull fetches and applies canonical hub events.
func (e *Engine) Pull(ctx context.Context) (*PullResultSummary, error) {
	return (&Puller{Config: e.Config}).Pull(ctx)
}

// SyncOnce runs one push pass followed by one pull pass.
func (e *Engine) SyncOnce(ctx context.Context) (push *PushResultSummary, pull *PullResultSummary, err error) {
	push, err = e.Push(ctx)
	if err != nil {
		return push, nil, err
	}
	pull, err = e.Pull(ctx)
	return push, pull, err
}
