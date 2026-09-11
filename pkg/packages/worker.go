package packages

import (
	"context"
	"fmt"
	"os"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// InstallWorker handles registry.package_install jobs.
type InstallWorker struct {
	Installer                   *Installer
	Store                       store.JobStore
	TerminologyInstalls         store.TerminologyInstallStoreFactory
	DefaultTerminologyTenantID  string
}

// HandleJob installs a package from the job payload.
func (w *InstallWorker) HandleJob(ctx context.Context, job store.JobRecord) error {
	if w == nil || w.Installer == nil {
		return fmt.Errorf("package install worker is not configured")
	}
	var payload jobs.PackageInstallPayload
	if err := jobs.UnmarshalPayload(job.Payload, &payload); err != nil {
		return err
	}
	ctx, err := registry.ContextWithJobTerminologyInstalls(ctx, job, w.TerminologyInstalls, w.DefaultTerminologyTenantID)
	if err != nil {
		return err
	}
	reporter := jobs.NewReporter(w.Store, job)
	installer := *w.Installer
	installer.OnProgress = func(current, total int, message string) {
		_ = reporter.Update(ctx, jobs.Progress{
			Phase:   "install",
			Current: current,
			Total:   total,
			Message: message,
		})
	}
	switch payload.Source {
	case "registry":
		result, err := installer.InstallFromRegistry(ctx, payload.PackageID, payload.Version)
		if err != nil {
			return err
		}
		return reporter.Complete(ctx, result)
	case "upload":
		f, err := os.Open(payload.Path)
		if err != nil {
			return fmt.Errorf("open uploaded package: %w", err)
		}
		result, installErr := installer.InstallFromArchive(ctx, payload.PackageID, payload.Version, f)
		_ = f.Close()
		_ = os.Remove(payload.Path)
		if installErr != nil {
			return installErr
		}
		return reporter.Complete(ctx, result)
	case "path":
		result, err := installer.InstallFromDirectory(ctx, payload.Path)
		if err != nil {
			return err
		}
		return reporter.Complete(ctx, result)
	default:
		return fmt.Errorf("unsupported package install source %q", payload.Source)
	}
}
