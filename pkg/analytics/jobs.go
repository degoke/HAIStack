package analytics

import (
	"context"
	"time"

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

// RefreshHandler returns a jobs.Handler that runs reporting refreshes using the
// supplied reporting target. When watermark is configured, it advances only after
// a successful refresh completes.
func RefreshHandler(runner *Runner, target reportingWriter, watermark *WatermarkStore) jobs.Handler {
	return jobs.HandlerFunc(func(ctx context.Context, job store.JobRecord) error {
		var payload RefreshPayload
		if err := jobs.UnmarshalPayload(job.Payload, &payload); err != nil {
			return err
		}
		version := payload.Version
		if version == "" {
			version = "1.0.0"
		}
		_, err := runner.Run(ctx, RunRequest{
			ViewName:    payload.ViewName,
			Version:     version,
			Mode:        ModeRefresh,
			Destination: Destination{Reporting: target},
			Actor:       payload.Actor,
			Subject:     payload.Subject,
			Incremental: true,
		})
		if err != nil {
			return err
		}
		if watermark != nil {
			refreshedAt := time.Now().UTC()
			if runner != nil && runner.now != nil {
				refreshedAt = runner.now().UTC()
			}
			return watermark.Advance(ctx, payload.ViewName, version, refreshedAt)
		}
		return nil
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
