package jobs

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// Handler processes one claimed background job.
type Handler interface {
	HandleJob(ctx context.Context, job store.JobRecord) error
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, job store.JobRecord) error

// HandleJob implements Handler.
func (f HandlerFunc) HandleJob(ctx context.Context, job store.JobRecord) error {
	return f(ctx, job)
}
