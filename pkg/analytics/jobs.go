package analytics

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/view"
)

// RefreshPayload is the job payload for analytics reporting refresh runs.
type RefreshPayload struct {
	ViewName string `json:"viewName"`
	Version  string `json:"version"`
	Actor    string `json:"actor,omitempty"`
	Subject  string `json:"subject,omitempty"`
}

// ExportPayload is the job payload for analytics export runs.
type ExportPayload struct {
	ViewName      string             `json:"viewName"`
	Version       string             `json:"version"`
	Actor         string             `json:"actor,omitempty"`
	Subject       string             `json:"subject,omitempty"`
	Since         time.Time          `json:"since,omitempty"`
	Parameters    map[string]any     `json:"parameters,omitempty"`
	Format        ExportFormat       `json:"format,omitempty"`
	ParquetLayout view.ParquetLayout `json:"parquetLayout,omitempty"`
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

// ExportHandler returns a jobs.Handler that runs export using the supplied sink.
// When watermark is configured, an empty Since is filled from the watermark and
// advanced after a successful export.
func ExportHandler(runner *Runner, sink RowSink, watermark *WatermarkStore) jobs.Handler {
	return jobs.HandlerFunc(func(ctx context.Context, job store.JobRecord) error {
		var payload ExportPayload
		if err := jobs.UnmarshalPayload(job.Payload, &payload); err != nil {
			return err
		}
		return runExportJob(ctx, runner, sink, watermark, payload)
	})
}

// ExportHandlerWithConfig returns a jobs.Handler that builds a sink per payload format.
func ExportHandlerWithConfig(runner *Runner, cfg ExportHandlerConfig, watermark *WatermarkStore) jobs.Handler {
	return jobs.HandlerFunc(func(ctx context.Context, job store.JobRecord) error {
		var payload ExportPayload
		if err := jobs.UnmarshalPayload(job.Payload, &payload); err != nil {
			return err
		}
		format := payload.Format
		if format == "" {
			format = FormatNDJSON
		}
		sink, err := cfg.buildSink(format, payload.ParquetLayout)
		if err != nil {
			return err
		}
		return runExportJob(ctx, runner, sink, watermark, payload)
	})
}

func runExportJob(ctx context.Context, runner *Runner, sink RowSink, watermark *WatermarkStore, payload ExportPayload) error {
	version := payload.Version
	if version == "" {
		version = "1.0.0"
	}
	if payload.Format == FormatParquet {
		if typed, ok := sink.(*ParquetFileSink); ok && payload.ParquetLayout != "" {
			typed.layout = payload.ParquetLayout
		}
	}
	since := payload.Since
	if since.IsZero() && watermark != nil {
		wmSince, err := watermark.Since(ctx, payload.ViewName, version)
		if err != nil {
			return err
		}
		since = wmSince
	}
	_, err := runner.Run(ctx, RunRequest{
		ViewName: payload.ViewName,
		Version:  version,
		Mode:     ModeExport,
		Destination: Destination{
			Sink: sink,
		},
		Actor:      payload.Actor,
		Subject:    payload.Subject,
		Parameters: payload.Parameters,
		Since:      since,
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
}

// ExportHandlerConfig configures dynamic export sinks for background jobs.
type ExportHandlerConfig struct {
	Writer   io.Writer
	Executor *view.Executor
	Actor    string
}

func (cfg ExportHandlerConfig) buildSink(format ExportFormat, layout view.ParquetLayout) (RowSink, error) {
	if cfg.Writer == nil {
		return nil, fmt.Errorf("%w: export writer is required", ErrUnsupportedDestination)
	}
	switch format {
	case FormatCSV:
		return NewCSVSink(cfg.Writer), nil
	case FormatParquet:
		if layout == "" {
			layout = view.ParquetLayoutFlat
		}
		return NewParquetFileSinkWithConfig(ParquetFileSinkConfig{
			Writer:   cfg.Writer,
			Layout:   layout,
			Executor: cfg.Executor,
			Actor:    cfg.Actor,
		}), nil
	default:
		return NewNDJSONSink(cfg.Writer), nil
	}
}
