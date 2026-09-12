package view

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"time"
)

// ViewRunRequest captures parameters for ViewDefinition/$viewdefinition-run.
type ViewRunRequest struct {
	ViewName      string
	Version       string
	InlineDef     []byte
	Since         time.Time
	Limit         int
	Offset        int
	Actor         string
	Subject       string
	Parameters    map[string]any
	Format        OutputFormat
	ParquetLayout ParquetLayout
	Header        bool
}

// RunService executes synchronous ViewDefinition runs.
type RunService struct {
	executor *Executor
}

// NewRunService returns a RunService.
func NewRunService(executor *Executor) *RunService {
	return &RunService{executor: executor}
}

// Execute runs a registered or inline ViewDefinition and encodes the result.
func (s *RunService) Execute(ctx context.Context, req ViewRunRequest) ([]byte, string, error) {
	if s == nil || s.executor == nil {
		return nil, "", fmt.Errorf("view: run service is not configured")
	}
	format := req.Format
	if format == "" {
		format = FormatJSON
	}

	execReq := ExecuteRequest{
		ViewName:   req.ViewName,
		Version:    req.Version,
		Actor:      req.Actor,
		Subject:    req.Subject,
		Limit:      req.Limit,
		Offset:     req.Offset,
		Parameters: req.Parameters,
		Since:      req.Since,
	}

	if format == FormatParquet && req.ParquetLayout == ParquetLayoutFHIR {
		if len(req.InlineDef) > 0 {
			return nil, "", fmt.Errorf("view: Parquet-on-FHIR layout requires a registered view")
		}
		var buf bytes.Buffer
		if _, err := WriteParquetFHIRExport(ctx, &buf, s.executor, execReq); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), ParquetContentType, nil
	}

	var result *Result
	var err error
	if len(req.InlineDef) > 0 {
		result, err = s.executor.ExecuteInline(ctx, execReq, req.InlineDef)
	} else {
		result, err = s.executor.Execute(ctx, execReq)
	}
	if err != nil {
		return nil, "", err
	}

	body, contentType, err := encodeRunResult(ctx, result, format, req.Header, req.ParquetLayout, s.executor, execReq)
	return body, contentType, err
}

// SQLQueryRequest carries one SQL-on-FHIR sqlquery-run request.
type SQLQueryRequest struct {
	SQL    string
	Tables []ReportingTableRef
	Format OutputFormat
}

// SQLQueryService executes SQL queries against reporting tables.
type SQLQueryService struct {
	engine *SQLQueryEngine
}

// NewSQLQueryService returns a SQLQueryService.
func NewSQLQueryService(engine *SQLQueryEngine) *SQLQueryService {
	return &SQLQueryService{engine: engine}
}

// Execute runs one SQL query and encodes the result.
func (s *SQLQueryService) Execute(ctx context.Context, req SQLQueryRequest) ([]byte, string, error) {
	if s == nil || s.engine == nil {
		return nil, "", fmt.Errorf("view: sql query service is not configured")
	}
	format := req.Format
	if format == "" {
		format = FormatJSON
	}
	result, err := s.engine.Query(ctx, req.SQL, req.Tables)
	if err != nil {
		return nil, "", err
	}
	viewResult := &Result{
		Columns: result.Columns,
		Rows:    result.Rows,
		Total:   len(result.Rows),
	}
	body, contentType, err := encodeRunResult(ctx, viewResult, format, true, ParquetLayoutFlat, nil, ExecuteRequest{})
	return body, contentType, err
}

// ParseLimitOffset parses _limit and _offset query parameters.
func ParseLimitOffset(limitRaw, offsetRaw string) (int, int, error) {
	limit := 0
	offset := 0
	if limitRaw != "" {
		parsed, err := strconv.Atoi(limitRaw)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid _limit parameter")
		}
		limit = parsed
	}
	if offsetRaw != "" {
		parsed, err := strconv.Atoi(offsetRaw)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid _offset parameter")
		}
		offset = parsed
	}
	return limit, offset, nil
}
