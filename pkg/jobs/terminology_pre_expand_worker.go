package jobs

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
)

// TerminologyPreExpandWorker handles registry.terminology.pre_expand_valuesets jobs.
type TerminologyPreExpandWorker struct {
	Terminology  store.TerminologyStore
	TenantScope  string
	MaxExpansion int
	Installs     store.TerminologyInstallStore
	JobStore     store.JobStore
}

// HandleJob pre-expands one or more ValueSets in a scope.
func (w *TerminologyPreExpandWorker) HandleJob(ctx context.Context, job store.JobRecord) error {
	if w == nil || w.Terminology == nil {
		return fmt.Errorf("terminology pre-expand worker is not configured")
	}
	var payload TerminologyPreExpandPayload
	if err := UnmarshalPayload(job.Payload, &payload); err != nil {
		return err
	}
	scope := payload.ScopeID
	if scope == "" {
		scope = w.TenantScope
	}
	if scope == "" {
		return fmt.Errorf("terminology pre-expand scope is required")
	}
	st := terminology.StoreForScope(w.Terminology, w.TenantScope, w.Installs, scope)
	reporter := NewReporter(w.JobStore, job)
	_ = reporter.Update(ctx, Progress{Phase: "pre-expand", Message: scope})

	opts := terminology.PreExpandOptions{
		MaxExpansion: w.MaxExpansion,
		Installs:     w.Installs,
	}
	if payload.URL != "" {
		res, err := terminology.PreExpandValueSet(ctx, st, scope, payload.URL, payload.Version, nil, opts)
		if err != nil {
			return err
		}
		return reporter.Complete(ctx, map[string]any{"results": []terminology.PreExpandResult{res}})
	}
	urls := payload.URLs
	results, err := terminology.PreExpandScope(ctx, st, scope, urls, opts)
	if err != nil {
		return err
	}
	expanded := 0
	for _, r := range results {
		if !r.Skipped {
			expanded++
		}
	}
	_ = reporter.Update(ctx, Progress{Phase: "pre-expand", Current: expanded, Total: len(results), Message: scope})
	return reporter.Complete(ctx, map[string]any{"expanded": expanded, "results": results})
}
