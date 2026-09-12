package view

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

// Config configures an Executor. The resource store and FHIRPath engine are
// required; authorizer, audit logger, and registry are optional but required for
// their respective features. If Now is nil, time.Now is used.
type Config struct {
	Resources         store.ResourceStore
	Engine            fhirpath.Engine
	Authorizer        Authorizer
	Audit             AuditLogger
	Registry          *Registry
	MaterializedViews store.MaterializedViewStore
	Search            search.Executor
	SearchRegistry    search.Registry
	SearchPlanner     search.Planner
	BaseURL           string
	ResolveLogicalID  func(ctx context.Context, logicalID string) (resourceType, id string, ok bool)
	ProfileCatalog    validate.ProfileCatalog
	Now               func() time.Time
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
	ViewName    string
	Version     string
	Actor       string
	Subject     string
	Limit       int
	Offset      int
	Parameters  map[string]any
	Since       time.Time
	Materialize bool
}

// Result is the structured output of a view execution.
type Result struct {
	ViewName    string
	Version     string
	Columns     []ColumnInfo
	Rows        []map[string]any
	Total       int
	Metadata    ResultMetadata
	NextOffset  *int
	ExecRequest *ExecuteRequest
}

// ExecRequestForExport returns the execute request used for Parquet-on-FHIR export.
func ExecRequestForExport(result *Result, actor string) ExecuteRequest {
	if result == nil {
		return ExecuteRequest{Actor: actor}
	}
	if result.ExecRequest != nil {
		req := *result.ExecRequest
		if req.Actor == "" {
			req.Actor = actor
		}
		return req
	}
	return ExecuteRequest{
		ViewName: result.ViewName,
		Version:  result.Version,
		Actor:    actor,
	}
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
	return e.executeSpec(ctx, req, spec, start)
}

// ExecuteInline runs an inline ViewDefinition payload with the same authorization,
// audit, and scan behavior as Execute.
func (e *Executor) ExecuteInline(ctx context.Context, req ExecuteRequest, def []byte) (*Result, error) {
	start := e.cfg.Now()
	spec, err := ParseDefinition(def, e.cfg.Engine)
	if err != nil {
		_ = e.logAudit(ctx, req, nil, "error", map[string]string{"error": err.Error()})
		return nil, err
	}
	return e.executeSpec(ctx, req, spec, start)
}

func (e *Executor) executeSpec(ctx context.Context, req ExecuteRequest, spec *ViewSpec, start time.Time) (*Result, error) {
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

	rows, scanned, filtered, err := e.executeScan(ctx, spec, req.Limit, req.Offset, req.Since)
	if err != nil {
		_ = e.logAudit(ctx, req, spec, "error", map[string]string{"error": err.Error()})
		return nil, err
	}

	materialize := req.Materialize || spec.Materialize
	if materialize {
		if e.cfg.MaterializedViews == nil {
			_ = e.logAudit(ctx, req, spec, "error", map[string]string{"error": ErrMissingMaterializedViewStore.Error()})
			return nil, ErrMissingMaterializedViewStore
		}
		allRows, _, _, scanErr := e.executeScan(ctx, spec, 0, 0, req.Since)
		if scanErr != nil {
			_ = e.logAudit(ctx, req, spec, "error", map[string]string{"error": scanErr.Error()})
			return nil, scanErr
		}
		if err := e.persistMaterializedRows(ctx, spec, allRows); err != nil {
			_ = e.logAudit(ctx, req, spec, "error", map[string]string{"error": err.Error()})
			return nil, err
		}
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

func (e *Executor) executeScan(ctx context.Context, spec *ViewSpec, limit, offset int, since time.Time) ([]map[string]any, int, int, error) {
	allIDs, err := e.resolveCandidateIDs(ctx, spec, since)
	if err != nil {
		return nil, 0, 0, err
	}

	scanned := len(allIDs)
	totalRows := 0
	rows := make([]map[string]any, 0)
	for _, id := range allIDs {
		env, err := e.cfg.Resources.Read(ctx, spec.ResourceType, id)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("read %s/%s: %w", spec.ResourceType, id, err)
		}
		if !since.IsZero() && !env.LastUpdated.IsZero() && env.LastUpdated.Before(since) {
			continue
		}
		match, err := e.evalFilters(ctx, spec, env)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("filter %s/%s: %w", spec.ResourceType, id, err)
		}
		if !match {
			continue
		}
		expanded, err := e.expandView(ctx, spec, env)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("expand %s/%s: %w", spec.ResourceType, id, err)
		}
		if len(expanded) == 0 {
			continue
		}
		for _, row := range expanded {
			if totalRows >= offset && (limit <= 0 || len(rows) < limit) {
				rows = append(rows, row)
			}
			totalRows++
		}
	}
	return rows, scanned, totalRows, nil
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

func (e *Executor) persistMaterializedRows(ctx context.Context, spec *ViewSpec, rows []map[string]any) error {
	if e.cfg.MaterializedViews == nil {
		return fmt.Errorf("%w: materialized view store is not configured", ErrMissingMaterializedViewStore)
	}
	keyColumn := spec.MaterializeKey
	if keyColumn == "" {
		keyColumn = "id"
	}
	viewKey := registryKey(spec.Name, spec.Version)
	now := e.cfg.Now()
	for i, row := range rows {
		key, err := materializedRowKey(row, keyColumn, i)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(row)
		if err != nil {
			return fmt.Errorf("marshal materialized row: %w", err)
		}
		if err := e.cfg.MaterializedViews.Upsert(ctx, store.MaterializedViewRecord{
			ViewName:  viewKey,
			Key:       key,
			Payload:   payload,
			Version:   1,
			UpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("upsert materialized row %q: %w", key, err)
		}
	}
	return nil
}

func materializedRowKey(row map[string]any, keyColumn string, index int) (string, error) {
	if val, ok := row[keyColumn]; ok && val != nil {
		switch v := val.(type) {
		case string:
			if v != "" {
				return v, nil
			}
		case fmt.Stringer:
			s := v.String()
			if s != "" {
				return s, nil
			}
		default:
			s := fmt.Sprint(v)
			if s != "" {
				return s, nil
			}
		}
	}
	return strconv.Itoa(index), nil
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
