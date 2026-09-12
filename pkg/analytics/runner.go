package analytics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/view"
)

// Mode selects the analytics execution target.
type Mode string

const (
	// ModeRefresh executes a view and writes to a reporting table (edge mode).
	ModeRefresh Mode = "refresh"
	// ModeExport executes a view and writes to a row sink (cloud mode).
	ModeExport Mode = "export"
)

// Destination configures where structured rows are written.
type Destination struct {
	Reporting reportingWriter
	Sink      RowSink
}

type reportingWriter interface {
	writeAt(ctx context.Context, result *view.Result, refreshedAt time.Time) error
}

// RunRequest carries parameters for one synchronous analytics run.
type RunRequest struct {
	ViewName    string
	Version     string
	Mode        Mode
	Destination Destination
	Actor       string
	Subject     string
	Parameters  map[string]any
	// Since limits view execution to resources updated after this timestamp.
	Since time.Time
	// Incremental enables cursor-based delta refresh when a cursor store is
	// configured on the destination reporting target.
	Incremental bool
}

// RunResult summarizes a completed analytics run.
type RunResult struct {
	ViewName string
	Version  string
	Mode     Mode
	RowCount int
	Metadata view.ResultMetadata
}

// Config configures a Runner.
type Config struct {
	Executor *view.Executor
	Now      func() time.Time
}

// Runner orchestrates view execution and hands rows to analytics targets.
type Runner struct {
	executor *view.Executor
	now      func() time.Time
}

// NewRunner validates configuration and returns a Runner.
func NewRunner(cfg Config) (*Runner, error) {
	if cfg.Executor == nil {
		return nil, ErrMissingExecutor
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Runner{
		executor: cfg.Executor,
		now:      cfg.Now,
	}, nil
}

// Run resolves and executes the named view, then writes rows to the configured target.
func (r *Runner) Run(ctx context.Context, req RunRequest) (*RunResult, error) {
	if !IsSupportedView(req.ViewName) {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedView, req.ViewName)
	}
	if err := validateDestination(req.Mode, req.Destination); err != nil {
		return nil, err
	}
	if err := r.validatePackagedView(req.ViewName, req.Version); err != nil {
		// Let Executor.Execute handle unresolved views so its normal audit and
		// error propagation path remains intact.
		if !errors.Is(err, view.ErrViewNotFound) {
			return nil, err
		}
	}
	runAt := r.now().UTC()

	since := req.Since
	if req.Mode == ModeRefresh && req.Incremental {
		if inc, ok := req.Destination.Reporting.(*IncrementalTarget); ok && since.IsZero() {
			cursorSince, sinceErr := inc.Since(ctx, req.ViewName, req.Version)
			if sinceErr != nil {
				return nil, sinceErr
			}
			since = cursorSince
		}
	}

	version := req.Version
	if version == "" {
		version = "1.0.0"
	}

	layout, hasLayout := sinkParquetLayout(req.Destination.Sink)
	skipFlatExecute := req.Mode == ModeExport && hasLayout && layout == view.ParquetLayoutFHIR

	var result *view.Result
	var err error
	if skipFlatExecute {
		result = &view.Result{
			ViewName: req.ViewName,
			Version:  version,
			Metadata: view.ResultMetadata{ExecutedAt: runAt},
			ExecRequest: &view.ExecuteRequest{
				ViewName:   req.ViewName,
				Version:    version,
				Actor:      req.Actor,
				Subject:    req.Subject,
				Parameters: req.Parameters,
				Since:      since,
			},
		}
	} else {
		result, err = r.executor.Execute(ctx, view.ExecuteRequest{
			ViewName:   req.ViewName,
			Version:    version,
			Actor:      req.Actor,
			Subject:    req.Subject,
			Parameters: req.Parameters,
			Since:      since,
		})
		if err != nil {
			return nil, err
		}
		result.Metadata.ExecutedAt = runAt
	}

	switch req.Mode {
	case ModeRefresh:
		if inc, ok := req.Destination.Reporting.(*IncrementalTarget); ok {
			if err := inc.writeAt(ctx, result, runAt); err != nil {
				return nil, err
			}
		} else if err := req.Destination.Reporting.writeAt(ctx, result, runAt); err != nil {
			return nil, err
		}
	case ModeExport:
		if err := req.Destination.Sink.WriteRows(ctx, result); err != nil {
			return nil, err
		}
	default:
		return nil, ErrUnsupportedMode
	}

	rowCount := len(result.Rows)
	if req.Mode == ModeExport {
		if count := lastExportRowCount(req.Destination.Sink); count > 0 {
			rowCount = count
		}
	}

	return &RunResult{
		ViewName: result.ViewName,
		Version:  result.Version,
		Mode:     req.Mode,
		RowCount: rowCount,
		Metadata: result.Metadata,
	}, nil
}

func validateDestination(mode Mode, dest Destination) error {
	if dest.Reporting != nil && dest.Sink != nil {
		return fmt.Errorf("%w: reporting target and row sink are mutually exclusive", ErrUnsupportedDestination)
	}
	switch mode {
	case ModeRefresh:
		if dest.Reporting == nil {
			return fmt.Errorf("%w: reporting target is required for refresh mode", ErrUnsupportedDestination)
		}
	case ModeExport:
		if dest.Sink == nil {
			return fmt.Errorf("%w: row sink is required for export mode", ErrUnsupportedDestination)
		}
	default:
		return ErrUnsupportedMode
	}
	return nil
}

func (r *Runner) validatePackagedView(name, version string) error {
	if version != "" && version != "1.0.0" {
		return fmt.Errorf("%w: %q version %q is not a packaged v1 view", ErrUnsupportedView, name, version)
	}
	spec, err := r.executor.ResolveView(name, version)
	if err != nil {
		return err
	}
	if !isPackagedView(spec) {
		return fmt.Errorf("%w: %q is not the packaged v1 definition", ErrUnsupportedView, name)
	}
	return nil
}
