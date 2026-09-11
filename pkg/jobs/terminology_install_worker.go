package jobs

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
)

// TerminologyInstallWorker handles registry.terminology.install jobs.
type TerminologyInstallWorker struct {
	Terminology  store.TerminologyStore
	ScopeID      string
	MaxExpansion int
	Installs     store.TerminologyInstallStore
	JobStore     store.JobStore
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
	if payload.PreExpandValueSets {
		_ = reporter.Update(ctx, Progress{Phase: "pre-expand", Message: scope})
		st := terminology.StoreForScope(w.Terminology, w.ScopeID, w.Installs, scope)
		results, err := terminology.PreExpandScope(ctx, st, scope, nil, terminology.PreExpandOptions{
			MaxExpansion: w.MaxExpansion,
			Installs:     w.Installs,
		})
		if err != nil {
			return err
		}
		expanded := 0
		for _, r := range results {
			if !r.Skipped {
				expanded++
			}
		}
		return reporter.Complete(ctx, map[string]any{"scope": scope, "expanded": expanded, "results": results})
	}
	return reporter.Complete(ctx, map[string]string{"scope": scope})
}
