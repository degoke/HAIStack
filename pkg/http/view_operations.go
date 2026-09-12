package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/view"
)

// ViewRunService handles ViewDefinition/$viewdefinition-run operations.
type ViewRunService interface {
	Execute(ctx context.Context, req view.ViewRunRequest) ([]byte, string, error)
}

// SQLQueryService handles Library/$sqlquery-run operations.
type SQLQueryService interface {
	Execute(ctx context.Context, req view.SQLQueryRequest) ([]byte, string, error)
}

// ViewExportService handles ViewDefinition/$viewdefinition-export operations.
type ViewExportService interface {
	Kickoff(ctx context.Context, req view.ViewExportRequest) (*view.ViewExportJob, error)
	GetJob(ctx context.Context, jobID string) (*view.ViewExportJob, error)
	Cancel(ctx context.Context, jobID string) error
	StatusURL(jobID string) string
	FileURL(jobID, filename string) string
	GetFile(ctx context.Context, jobID, filename string) ([]byte, string, error)
}

func (h *handler) handleViewDefinitionRun(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if h.cfg.ViewRunService == nil {
		writeError(w, notImplementedEndpoint(r.URL.Path))
		return
	}
	if route.resourceType != "" && route.resourceType != "ViewDefinition" {
		writeError(w, unsupportedEndpoint(r.URL.Path))
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodPost)
		return
	}
	if err := h.authorizeWrite(r.Context(), "operation", "ViewDefinition", route.id); err != nil {
		writeError(w, err)
		return
	}
	req, err := parseViewRunRequest(r, route)
	if err != nil {
		writeError(w, err)
		return
	}
	body, contentType, err := h.cfg.ViewRunService.Execute(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *handler) handleSQLQueryRun(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if h.cfg.SQLQueryService == nil {
		writeError(w, notImplementedEndpoint(r.URL.Path))
		return
	}
	if route.resourceType != "" && route.resourceType != "Library" {
		writeError(w, unsupportedEndpoint(r.URL.Path))
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodPost)
		return
	}
	if err := h.authorizeWrite(r.Context(), "operation", "Library", route.id); err != nil {
		writeError(w, err)
		return
	}
	req, err := parseSQLQueryRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}
	body, contentType, err := h.cfg.SQLQueryService.Execute(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *handler) handleViewDefinitionExport(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if h.cfg.ViewExportService == nil {
		writeError(w, notImplementedEndpoint(r.URL.Path))
		return
	}
	if route.resourceType != "ViewDefinition" {
		writeError(w, unsupportedEndpoint(r.URL.Path))
		return
	}
	switch r.Method {
	case http.MethodPost:
		if err := h.authorizeWrite(r.Context(), "operation", "ViewDefinition", route.id); err != nil {
			writeError(w, err)
			return
		}
		if strings.ToLower(r.Header.Get("Prefer")) != "respond-async" {
			writeError(w, invalidRequest("Prefer: respond-async is required for ViewDefinition/$viewdefinition-export", nil))
			return
		}
		req, err := parseViewExportRequest(r, route)
		if err != nil {
			writeError(w, err)
			return
		}
		job, err := h.cfg.ViewExportService.Kickoff(r.Context(), req)
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Content-Location", h.cfg.ViewExportService.StatusURL(job.ID))
		w.WriteHeader(http.StatusAccepted)
	default:
		writeMethodNotAllowed(w, r.Method, http.MethodPost)
	}
}

func (h *handler) handleViewDefinitionExportStatus(w http.ResponseWriter, r *http.Request, jobID string) {
	if h.cfg.ViewExportService == nil {
		writeError(w, notImplementedEndpoint(r.URL.Path))
		return
	}
	if err := h.authorizeWrite(r.Context(), "operation", "ViewDefinition", ""); err != nil {
		writeError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		job, err := h.cfg.ViewExportService.GetJob(r.Context(), jobID)
		if err != nil {
			writeError(w, notFound("view export job %q not found", jobID))
			return
		}
		switch job.Status {
		case view.ExportComplete:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(enrichViewExportJob(h.cfg.ViewExportService, job))
		case view.ExportInProgress:
			if job.Progress != "" {
				w.Header().Set("X-Progress", job.Progress)
			}
			w.WriteHeader(http.StatusAccepted)
		case view.ExportCancelled:
			writeError(w, invalidRequest("view export job cancelled", nil))
		default:
			writeError(w, invalidRequest(job.LastError, nil))
		}
	case http.MethodDelete:
		if err := h.cfg.ViewExportService.Cancel(r.Context(), jobID); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	default:
		writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodDelete)
	}
}

func (h *handler) handleViewDefinitionExportFile(w http.ResponseWriter, r *http.Request, jobID, filename string) {
	if h.cfg.ViewExportService == nil {
		writeError(w, notImplementedEndpoint(r.URL.Path))
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r.Method, http.MethodGet)
		return
	}
	if err := h.authorizeWrite(r.Context(), "operation", "ViewDefinition", ""); err != nil {
		writeError(w, err)
		return
	}
	data, contentType, err := h.cfg.ViewExportService.GetFile(r.Context(), jobID, filename)
	if err != nil {
		writeError(w, notFound("view export file not found"))
		return
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

type viewExportJobResponse struct {
	view.ViewExportJob
	Files []viewExportFileResponse `json:"files"`
}

type viewExportFileResponse struct {
	view.ExportFile
	URL string `json:"url"`
}

func enrichViewExportJob(svc ViewExportService, job *view.ViewExportJob) viewExportJobResponse {
	resp := viewExportJobResponse{ViewExportJob: *job}
	resp.Files = make([]viewExportFileResponse, len(job.Files))
	for i, file := range job.Files {
		resp.Files[i] = viewExportFileResponse{
			ExportFile: file,
			URL:        svc.FileURL(job.ID, file.Filename),
		}
	}
	return resp
}

func parseViewRunRequest(r *http.Request, route parsedRoute) (view.ViewRunRequest, error) {
	req := view.ViewRunRequest{
		ViewName: route.id,
		Version:  strings.TrimSpace(r.URL.Query().Get("version")),
		Format:   view.ParseOutputFormat(r.URL.Query().Get("_format")),
		ParquetLayout: view.ParseParquetLayout(firstNonEmpty(
			r.URL.Query().Get("_parquetLayout"),
			r.URL.Query().Get("parquetLayout"),
		)),
		Header:   strings.EqualFold(r.URL.Query().Get("header"), "true"),
	}
	if since := r.URL.Query().Get("_since"); since != "" {
		parsed, err := time.Parse(time.RFC3339, since)
		if err != nil {
			return req, invalidRequest("invalid _since parameter", err)
		}
		req.Since = parsed.UTC()
	}
	limit, offset, err := view.ParseLimitOffset(r.URL.Query().Get("_limit"), r.URL.Query().Get("_offset"))
	if err != nil {
		return req, invalidRequest(err.Error(), nil)
	}
	req.Limit = limit
	req.Offset = offset
	if req.ViewName == "" {
		req.ViewName = strings.TrimSpace(r.URL.Query().Get("viewName"))
	}
	if r.Body == nil || r.ContentLength == 0 {
		if req.ViewName == "" {
			return req, invalidRequest("viewName is required", nil)
		}
		return req, nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return req, invalidRequest("invalid request body", err)
	}
	var params struct {
		Parameter []struct {
			Name  string `json:"name"`
			Value *struct {
				String string `json:"valueString"`
			} `json:"valueString"`
		} `json:"parameter"`
		Resource json.RawMessage `json:"resource"`
	}
	if err := json.Unmarshal(body, &params); err != nil {
		return req, invalidRequest("invalid Parameters body", err)
	}
	for _, p := range params.Parameter {
		if p.Value == nil {
			continue
		}
		switch p.Name {
		case "viewName":
			if req.ViewName == "" {
				req.ViewName = p.Value.String
			}
		case "version":
			req.Version = p.Value.String
		}
	}
	if len(params.Resource) > 0 {
		req.InlineDef = params.Resource
	}
	if req.ViewName == "" && len(req.InlineDef) == 0 {
		return req, invalidRequest("viewName or inline ViewDefinition is required", nil)
	}
	return req, nil
}

func parseSQLQueryRequest(r *http.Request) (view.SQLQueryRequest, error) {
	req := view.SQLQueryRequest{
		Format: view.ParseOutputFormat(r.URL.Query().Get("_format")),
	}
	req.SQL = strings.TrimSpace(r.URL.Query().Get("sql"))
	if req.SQL == "" {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return req, invalidRequest("invalid request body", err)
		}
		var params struct {
			Parameter []struct {
				Name  string `json:"name"`
				Value *struct {
					String string `json:"valueString"`
				} `json:"valueString"`
			} `json:"parameter"`
		}
		if err := json.Unmarshal(body, &params); err != nil {
			return req, invalidRequest("invalid Parameters body", err)
		}
		for _, p := range params.Parameter {
			if p.Value == nil {
				continue
			}
			switch p.Name {
			case "sql":
				req.SQL = p.Value.String
			case "viewName":
				req.Tables = append(req.Tables, view.ReportingTableRef{ViewName: p.Value.String})
			}
		}
	}
	if req.SQL == "" {
		return req, invalidRequest("sql parameter is required", nil)
	}
	return req, nil
}

func parseViewExportRequest(r *http.Request, route parsedRoute) (view.ViewExportRequest, error) {
	req := view.ViewExportRequest{
		Format: view.ParseOutputFormat(r.URL.Query().Get("_format")),
		ParquetLayout: view.ParseParquetLayout(firstNonEmpty(
			r.URL.Query().Get("_parquetLayout"),
			r.URL.Query().Get("parquetLayout"),
		)),
	}
	if since := r.URL.Query().Get("_since"); since != "" {
		parsed, err := time.Parse(time.RFC3339, since)
		if err != nil {
			return req, invalidRequest("invalid _since parameter", err)
		}
		req.Since = parsed.UTC()
	}
	if route.id != "" {
		req.Views = []view.ViewExportTarget{{ViewName: route.id, Version: strings.TrimSpace(r.URL.Query().Get("version"))}}
	}
	if r.Body == nil || r.ContentLength == 0 {
		if len(req.Views) == 0 {
			viewName := strings.TrimSpace(r.URL.Query().Get("viewName"))
			if viewName == "" {
				return req, invalidRequest("viewName is required", nil)
			}
			req.Views = []view.ViewExportTarget{{ViewName: viewName, Version: strings.TrimSpace(r.URL.Query().Get("version"))}}
		}
		return req, nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return req, invalidRequest("invalid request body", err)
	}
	var params struct {
		Parameter []struct {
			Name    string `json:"name"`
			Part    []struct {
				Name  string `json:"name"`
				Value *struct {
					String string `json:"valueString"`
				} `json:"valueString"`
			} `json:"part"`
		} `json:"parameter"`
	}
	if err := json.Unmarshal(body, &params); err != nil {
		return req, invalidRequest("invalid Parameters body", err)
	}
	for _, p := range params.Parameter {
		if p.Name != "view" {
			continue
		}
		target := view.ViewExportTarget{}
		for _, part := range p.Part {
			if part.Value == nil {
				continue
			}
			switch part.Name {
			case "viewName", "name":
				target.ViewName = part.Value.String
			case "version":
				target.Version = part.Value.String
			case "outputName":
				target.OutputName = part.Value.String
			}
		}
		if target.ViewName != "" {
			req.Views = append(req.Views, target)
		}
	}
	if len(req.Views) == 0 {
		return req, invalidRequest("at least one view parameter is required", nil)
	}
	return req, nil
}
