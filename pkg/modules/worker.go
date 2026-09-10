package modules

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// InstallWorker handles modules.install jobs.
type InstallWorker struct {
	Manager *Manager
	Store   store.JobStore
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
	reporter := jobs.NewReporter(w.Store, job)
	_ = reporter.Update(ctx, jobs.Progress{Phase: "plan", Message: payload.Path})

	if payload.UpgradeOnly {
		result, err := w.Manager.Upgrade(ctx, payload.Path)
		if err != nil {
			return err
		}
		return reporter.Complete(ctx, result)
	}
	result, err := w.Manager.Install(ctx, payload.Path)
	if err != nil {
		return err
	}
	return reporter.Complete(ctx, result)
}
