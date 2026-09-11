package modules

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// InstallWorker handles modules.install jobs.
type InstallWorker struct {
	Manager                    *Manager
	Store                      store.JobStore
	TerminologyInstalls        store.TerminologyInstallStoreFactory
	DefaultTerminologyTenantID string
}

// HandleJob installs a module from a local directory path.
func (w *InstallWorker) HandleJob(ctx context.Context, job store.JobRecord) error {
	if w == nil || w.Manager == nil {
		return fmt.Errorf("module install worker is not configured")
	}
	var payload jobs.ModuleInstallPayload
	if err := jobs.UnmarshalPayload(job.Payload, &payload); err != nil {
		return err
	}
	if payload.Path == "" {
		return fmt.Errorf("module install path is required")
	}
	ctx, err := registry.ContextWithJobTerminologyInstalls(ctx, job, w.TerminologyInstalls, w.DefaultTerminologyTenantID)
	if err != nil {
		return err
	}
	reporter := jobs.NewReporter(w.Store, job)
	progress := func(current, total int, message string) {
		_ = reporter.Update(ctx, jobs.Progress{
			Phase:   "install",
			Current: current,
			Total:   total,
			Message: message,
		})
	}
	_ = reporter.Update(ctx, jobs.Progress{Phase: "plan", Message: payload.Path})

	if payload.UpgradeOnly {
		result, err := w.Manager.UpgradeWithProgress(ctx, payload.Path, progress)
		if err != nil {
			return err
		}
		return reporter.Complete(ctx, result)
	}
	result, err := w.Manager.InstallWithProgress(ctx, payload.Path, progress)
	if err != nil {
		return err
	}
	return reporter.Complete(ctx, result)
}
