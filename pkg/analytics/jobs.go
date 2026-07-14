package analytics

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// RefreshPayload is the job payload for analytics reporting refresh runs.
type RefreshPayload struct {
	ViewName string `json:"viewName"`
	Version  string `json:"version"`
	Actor    string `json:"actor,omitempty"`
	Subject  string `json:"subject,omitempty"`
}

// ExportPayload is the job payload for CSV export runs.
type ExportPayload struct {
	ViewName string `json:"viewName"`
	Version  string `json:"version"`
	Actor    string `json:"actor,omitempty"`
	Subject  string `json:"subject,omitempty"`
}

// Job type constants re-exported for analytics orchestration.
const (
	TypeRefresh = jobs.TypeAnalyticsRefresh
	TypeExport  = jobs.TypeExportCSV
)

// RefreshHandler returns a jobs.Handler that runs full reporting refreshes using
// the supplied reporting target.
func RefreshHandler(runner *Runner, target *ReportingTarget) jobs.Handler {
	return jobs.HandlerFunc(func(ctx context.Context, job store.JobRecord) error {
		var payload RefreshPayload
		if err := jobs.UnmarshalPayload(job.Payload, &payload); err != nil {
			return err
		}
		_, err := runner.Run(ctx, RunRequest{
			ViewName: payload.ViewName,
			Version:  payload.Version,
			Mode:     ModeRefresh,
			Destination: Destination{
				Reporting: target,
			},
			Actor:   payload.Actor,
			Subject: payload.Subject,
		})
		return err
	})
}

// ExportHandler returns a jobs.Handler that runs CSV export using the supplied sink.
func ExportHandler(runner *Runner, sink RowSink) jobs.Handler {
	return jobs.HandlerFunc(func(ctx context.Context, job store.JobRecord) error {
		var payload ExportPayload
		if err := jobs.UnmarshalPayload(job.Payload, &payload); err != nil {
			return err
		}
		_, err := runner.Run(ctx, RunRequest{
			ViewName: payload.ViewName,
			Version:  payload.Version,
			Mode:     ModeExport,
			Destination: Destination{
				Sink: sink,
			},
			Actor:   payload.Actor,
			Subject: payload.Subject,
		})
		return err
	})
}
