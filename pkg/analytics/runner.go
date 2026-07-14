package analytics

import (
	"context"
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
	Reporting *ReportingTarget
	Sink      RowSink
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

	result, err := r.executor.Execute(ctx, view.ExecuteRequest{
		ViewName:   req.ViewName,
		Version:    req.Version,
		Actor:      req.Actor,
		Subject:    req.Subject,
		Parameters: req.Parameters,
	})
	if err != nil {
		return nil, err
	}

	switch req.Mode {
	case ModeRefresh:
		if err := req.Destination.Reporting.Write(ctx, result); err != nil {
			return nil, err
		}
	case ModeExport:
		if err := req.Destination.Sink.WriteRows(ctx, result); err != nil {
			return nil, err
		}
	default:
		return nil, ErrUnsupportedMode
	}

	return &RunResult{
		ViewName: result.ViewName,
		Version:  result.Version,
		Mode:     req.Mode,
		RowCount: len(result.Rows),
		Metadata: result.Metadata,
	}, nil
}

func validateDestination(mode Mode, dest Destination) error {
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
