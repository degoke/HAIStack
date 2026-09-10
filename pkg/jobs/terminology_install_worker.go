package jobs

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
)

// TerminologyInstallWorker handles registry.terminology.install jobs.
type TerminologyInstallWorker struct {
	Terminology store.TerminologyStore
	ScopeID     string
	JobStore    store.JobStore
}

// HandleJob rebuilds terminology projections for the configured scope.
func (w *TerminologyInstallWorker) HandleJob(ctx context.Context, job store.JobRecord) error {
	if w == nil || w.Terminology == nil {
		return fmt.Errorf("terminology install worker is not configured")
	}
	var payload TerminologyInstallPayload
	if err := UnmarshalPayload(job.Payload, &payload); err != nil {
		return err
	}
	scope := payload.ScopeID
	if scope == "" {
		scope = w.ScopeID
	}
	if scope == "" {
		return fmt.Errorf("terminology install scope is required")
	}
	reporter := NewReporter(w.JobStore, job)
	_ = reporter.Update(ctx, Progress{Phase: "rebuild", Message: scope})
	if err := terminology.Rebuild(ctx, w.Terminology, scope); err != nil {
		return err
	}
	return reporter.Complete(ctx, map[string]string{"scope": scope})
}
