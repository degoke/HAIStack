package http

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
)

func enqueueJob(ctx context.Context, jobStore store.JobStore, jobType string, payload any, opts jobs.EnqueueOptions) (store.JobRecord, error) {
	if principal, tenant, ok := identityFromContext(ctx); ok {
		opts.PrincipalID = principal.ID
		opts.TenantID = tenant.TenantID
	}
	return jobs.Enqueue(ctx, jobStore, jobType, payload, opts)
}
