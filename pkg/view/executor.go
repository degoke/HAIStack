package view

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// Config configures an Executor. The resource store and FHIRPath engine are
// required; authorizer, audit logger, and registry are optional but required for
// their respective features. If Now is nil, time.Now is used.
type Config struct {
	Resources  store.ResourceStore
	Engine     fhirpath.Engine
	Authorizer Authorizer
	Audit      AuditLogger
	Registry   *Registry
	Now        func() time.Time
}

// Executor runs a parsed ViewDefinition against a store.ResourceStore.
type Executor struct {
	cfg Config
	enc *RowEncoder
}

// ResolveView resolves a registered view without executing it. It is useful to
// validate an execution policy before touching the resource store.
func (e *Executor) ResolveView(name, version string) (*ViewSpec, error) {
	if e == nil || e.cfg.Registry == nil {
		return nil, fmt.Errorf("%w: executor requires a registry to resolve views", ErrViewNotFound)
	}
	return e.cfg.Registry.Resolve(name, version)
}

// ExecuteRequest carries runtime parameters for one view execution.
type ExecuteRequest struct {
	ViewName   string
	Version    string
	Actor      string
	Subject    string
	Limit      int
	Offset     int
	Parameters map[string]any
}

// Result is the structured output of a view execution.
type Result struct {
	ViewName   string
	Version    string
	Columns    []ColumnInfo
	Rows       []map[string]any
	Total      int
	Metadata   ResultMetadata
	NextOffset *int
}

// ResultMetadata captures execution-side metadata.
type ResultMetadata struct {
	ExecutedAt         time.Time     `json:"executedAt"`
	Duration           time.Duration `json:"duration"`
	SourceResourceType string        `json:"sourceResourceType"`
	Scanned            int           `json:"scanned"`
	Filtered           int           `json:"filtered"`
}

// NewExecutor validates the configuration and returns an Executor.
func NewExecutor(cfg Config) (*Executor, error) {
	if cfg.Resources == nil {
		return nil, ErrMissingResourceStore
	}
	if cfg.Engine == nil {
		return nil, ErrMissingEngine
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Executor{
		cfg: cfg,
		enc: newRowEncoder(),
	}, nil
}

// Execute resolves the registered view, applies optional authorization, scans the
// source resource type, filters resources, extracts columns, and returns rows.
// Audit records are written for both success and denial when an Audit logger is
// configured.
func (e *Executor) Execute(ctx context.Context, req ExecuteRequest) (*Result, error) {
	start := e.cfg.Now()
	spec, err := e.ResolveView(req.ViewName, req.Version)
	if err != nil {
		_ = e.logAudit(ctx, req, spec, "error", map[string]string{"error": err.Error()})
		return nil, err
	}

	if e.cfg.Authorizer != nil && len(spec.Permissions) > 0 {
		if err := e.cfg.Authorizer.AuthorizeView(ctx, AuthRequest{
			ViewName:     spec.Name,
			Version:      spec.Version,
			ResourceType: spec.ResourceType,
			Actor:        req.Actor,
			Subject:      req.Subject,
			Permissions:  spec.Permissions,
			Parameters:   req.Parameters,
		}); err != nil {
			_ = e.logAudit(ctx, req, spec, "denied", map[string]string{"error": err.Error()})
			return nil, fmt.Errorf("%w: %v", ErrUnauthorized, err)
		}
	}

	if err := spec.compile(e.cfg.Engine); err != nil {
		_ = e.logAudit(ctx, req, spec, "error", map[string]string{"error": err.Error()})
		return nil, err
	}

	rows, scanned, filtered, err := e.executeScan(ctx, spec, req.Limit, req.Offset)
	if err != nil {
		_ = e.logAudit(ctx, req, spec, "error", map[string]string{"error": err.Error()})
		return nil, err
	}

	metadata := ResultMetadata{
		ExecutedAt:         e.cfg.Now(),
		Duration:           e.cfg.Now().Sub(start),
		SourceResourceType: spec.ResourceType,
		Scanned:            scanned,
		Filtered:           filtered,
	}

	var nextOffset *int
	if req.Offset+len(rows) < filtered {
		n := req.Offset + len(rows)
		nextOffset = &n
	}

	res := &Result{
		ViewName:   spec.Name,
		Version:    spec.Version,
		Columns:    spec.ColumnInfos(),
		Rows:       rows,
		Total:      filtered,
		Metadata:   metadata,
		NextOffset: nextOffset,
	}

	_ = e.logAudit(ctx, req, spec, "success", map[string]string{
		"scanned":  fmt.Sprintf("%d", scanned),
		"filtered": fmt.Sprintf("%d", filtered),
		"returned": fmt.Sprintf("%d", len(rows)),
	})
	return res, nil
}

func (e *Executor) executeScan(ctx context.Context, spec *ViewSpec, limit, offset int) ([]map[string]any, int, int, error) {
	var allIDs []string
	pageSize := 100
	if limit > 0 && limit > pageSize {
		pageSize = limit * 2
	}
	if pageSize <= 0 {
		pageSize = 100
	}
	for {
		ids, err := e.cfg.Resources.ListIDs(ctx, spec.ResourceType, pageSize, len(allIDs))
		if err != nil {
			return nil, 0, 0, fmt.Errorf("list %s IDs: %w", spec.ResourceType, err)
		}
		if len(ids) == 0 {
			break
		}
		allIDs = append(allIDs, ids...)
	}

	scanned := 0
	filtered := 0
	rows := make([]map[string]any, 0)
	for _, id := range allIDs {
		scanned++
		env, err := e.cfg.Resources.Read(ctx, spec.ResourceType, id)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("read %s/%s: %w", spec.ResourceType, id, err)
		}
		match, err := e.evalFilters(ctx, spec, env)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("filter %s/%s: %w", spec.ResourceType, id, err)
		}
		if !match {
			continue
		}
		filtered++
		if filtered <= offset || (limit > 0 && len(rows) >= limit) {
			continue
		}
		row, err := e.evalColumns(ctx, spec, env)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("extract %s/%s: %w", spec.ResourceType, id, err)
		}
		rows = append(rows, row)
	}
	return rows, scanned, filtered, nil
}

func (e *Executor) evalFilters(ctx context.Context, spec *ViewSpec, resource any) (bool, error) {
	for _, filter := range spec.Filters {
		ok, err := filter.compiled.EvalBool(ctx, resource)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func (e *Executor) evalColumns(ctx context.Context, spec *ViewSpec, resource any) (map[string]any, error) {
	row := make(map[string]any, len(spec.Columns))
	for _, col := range spec.Columns {
		values, err := col.compiled.Eval(ctx, resource)
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", col.Name, err)
		}
		encoded, err := e.enc.EncodeColumn(values, col.Collection)
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", col.Name, err)
		}
		row[col.Name] = encoded
	}
	return row, nil
}

func (e *Executor) logAudit(ctx context.Context, req ExecuteRequest, spec *ViewSpec, outcome string, details map[string]string) error {
	if e.cfg.Audit == nil {
		return nil
	}
	viewName := req.ViewName
	version := req.Version
	if spec != nil {
		viewName = spec.Name
		version = spec.Version
	}
	return e.cfg.Audit.LogViewAccess(ctx, AuditRecord{
		ViewName:   viewName,
		Version:    version,
		Actor:      req.Actor,
		Subject:    req.Subject,
		Outcome:    outcome,
		Details:    details,
		Parameters: req.Parameters,
		Timestamp:  e.cfg.Now(),
	})
}
