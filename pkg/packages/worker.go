package packages

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// InstallWorker handles registry.package_install jobs.
type InstallWorker struct {
	Installer *Installer
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
	switch payload.Source {
	case "registry":
		_, err := w.Installer.InstallFromRegistry(ctx, payload.PackageID, payload.Version)
		return err
	case "path":
		_, err := w.Installer.InstallFromDirectory(ctx, payload.Path)
		return err
	default:
		return fmt.Errorf("unsupported package install source %q", payload.Source)
	}
}
