package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/view"
)

// ViewMaterializeService handles ViewDefinition/$materialize operations.
type ViewMaterializeService interface {
	Kickoff(ctx context.Context, req view.MaterializeRequest) (*view.MaterializeJob, error)
	GetJob(ctx context.Context, jobID string) (*view.MaterializeJob, error)
	Cancel(ctx context.Context, jobID string) error
	StatusURL(jobID string) string
	Result(job *view.MaterializeJob) *view.MaterializeResult
}

func (h *handler) handleViewMaterialize(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if h.cfg.ViewMaterializeService == nil {
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
			writeError(w, invalidRequest("Prefer: respond-async is required for ViewDefinition/$materialize", nil))
			return
		}
		req, err := parseMaterializeRequest(r, route)
		if err != nil {
			writeError(w, err)
			return
		}
		job, err := h.cfg.ViewMaterializeService.Kickoff(r.Context(), req)
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Content-Location", h.cfg.ViewMaterializeService.StatusURL(job.ID))
		w.WriteHeader(http.StatusAccepted)
	default:
		writeMethodNotAllowed(w, r.Method, http.MethodPost)
	}
}

func (h *handler) handleViewMaterializeStatus(w http.ResponseWriter, r *http.Request, jobID string) {
	if h.cfg.ViewMaterializeService == nil {
		writeError(w, notImplementedEndpoint(r.URL.Path))
		return
	}
	if err := h.authorizeWrite(r.Context(), "operation", "ViewDefinition", ""); err != nil {
		writeError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		job, err := h.cfg.ViewMaterializeService.GetJob(r.Context(), jobID)
		if err != nil {
			writeError(w, notFound("materialize job %q not found", jobID))
			return
		}
		switch job.Status {
		case view.MaterializeComplete:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(h.cfg.ViewMaterializeService.Result(job))
		case view.MaterializeInProgress:
			if job.Progress != "" {
				w.Header().Set("X-Progress", job.Progress)
			}
			w.WriteHeader(http.StatusAccepted)
		case view.MaterializeCancelled:
			writeError(w, invalidRequest("materialize job cancelled", nil))
		default:
			writeError(w, invalidRequest(job.LastError, nil))
		}
	case http.MethodDelete:
		if err := h.cfg.ViewMaterializeService.Cancel(r.Context(), jobID); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	default:
		writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodDelete)
	}
}

func parseMaterializeRequest(r *http.Request, route parsedRoute) (view.MaterializeRequest, error) {
	req := view.MaterializeRequest{}
	if route.id != "" {
		req.ViewName = route.id
	}
	if since := r.URL.Query().Get("_since"); since != "" {
		parsed, err := time.Parse(time.RFC3339, since)
		if err != nil {
			return req, invalidRequest("invalid _since parameter", err)
		}
		req.Since = parsed.UTC()
	}
	req.TargetName = strings.TrimSpace(r.URL.Query().Get("targetName"))
	if req.ViewName == "" {
		req.ViewName = strings.TrimSpace(r.URL.Query().Get("viewName"))
	}
	req.Version = strings.TrimSpace(r.URL.Query().Get("version"))
	if r.Body == nil || r.ContentLength == 0 {
		if req.ViewName == "" {
			return req, invalidRequest("viewName is required", nil)
		}
		return req, nil
	}
	var params struct {
		Parameter []struct {
			Name  string `json:"name"`
			Value *struct {
				String string `json:"valueString"`
			} `json:"valueString"`
		} `json:"parameter"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
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
		case "targetName":
			if req.TargetName == "" {
				req.TargetName = p.Value.String
			}
		}
	}
	if req.ViewName == "" {
		return req, invalidRequest("viewName is required", nil)
	}
	return req, nil
}
