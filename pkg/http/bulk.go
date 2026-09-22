package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/degoke/haistack/pkg/binary"
	"github.com/degoke/haistack/pkg/core"
	"github.com/degoke/haistack/pkg/export"
)

// BulkExportService handles FHIR Bulk Data export kickoff, polling, cancellation,
// manifest retrieval, and artifact download.
type BulkExportService interface {
	Kickoff(ctx context.Context, req export.KickoffRequest) (*export.Job, error)
	GetJob(ctx context.Context, jobID string) (*export.Job, error)
	Cancel(ctx context.Context, jobID string) error
	Manifest(job *export.Job) *export.Manifest
	StatusURL(jobID string) string
	GetFile(ctx context.Context, jobID, filename string) ([]byte, string, error)
	OpenFile(ctx context.Context, jobID, filename string) (io.ReadCloser, string, error)
}

func (h *handler) handleBulkExport(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if h.cfg.BulkExportService == nil {
		writeError(w, notImplementedEndpoint(r.URL.Path))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if err := h.authorizeExport(r.Context(), route.id); err != nil {
			writeError(w, err)
			return
		}
		if !prefersRespondAsync(r.Header.Get("Prefer")) {
			writeError(w, invalidRequest("Prefer: respond-async is required for bulk export kickoff", nil))
			return
		}
		req := export.KickoffRequest{
			ResourceTypes:  parseCSVParam(r.URL.Query().Get("_type")),
			TypeFilter:     r.URL.Query().Get("_typeFilter"),
			OutputFormat:   r.URL.Query().Get("_outputFormat"),
			RequestURL:     r.Method + " " + r.URL.RequestURI(),
			RequiresAccess: h.cfg.AuthChecker != nil,
		}
		if since := r.URL.Query().Get("_since"); since != "" {
			parsed, err := time.Parse(time.RFC3339, since)
			if err != nil {
				writeError(w, invalidRequest("invalid _since parameter", err))
				return
			}
			req.Since = parsed.UTC()
		}
		if route.resourceType == "Group" && route.id != "" {
			req.GroupID = route.id
		}
		if route.resourceType == "Patient" {
			if route.id != "" {
				req.PatientID = route.id
			} else {
				req.PatientExport = true
			}
		}
		if principal, tenant, ok := identityFromContext(r.Context()); ok {
			req.TenantID = tenant.TenantID
			req.PrincipalID = principal.ID
		}
		job, err := h.cfg.BulkExportService.Kickoff(r.Context(), req)
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Content-Location", h.cfg.BulkExportService.StatusURL(job.ID))
		w.WriteHeader(http.StatusAccepted)
	default:
		writeMethodNotAllowed(w, r.Method, http.MethodGet)
	}
}

func (h *handler) handleBulkExportStatus(w http.ResponseWriter, r *http.Request, jobID string) {
	if h.cfg.BulkExportService == nil {
		writeError(w, notImplementedEndpoint(r.URL.Path))
		return
	}
	if err := h.authorizeExport(r.Context(), ""); err != nil {
		writeError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		job, err := h.cfg.BulkExportService.GetJob(r.Context(), jobID)
		if err != nil {
			writeError(w, err)
			return
		}
		if job == nil {
			writeError(w, notFound("export job %q not found", jobID))
			return
		}
		switch job.Status {
		case export.StatusComplete:
			manifest := h.cfg.BulkExportService.Manifest(job)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(manifest)
		case export.StatusInProgress:
			if job.Progress != "" {
				w.Header().Set("X-Progress", job.Progress)
			}
			w.WriteHeader(http.StatusAccepted)
		case export.StatusCancelled:
			writeError(w, invalidRequest("export job cancelled", nil))
		default:
			writeError(w, invalidRequest(job.LastError, nil))
		}
	case http.MethodDelete:
		if err := h.cfg.BulkExportService.Cancel(r.Context(), jobID); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	default:
		writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodDelete)
	}
}

func (h *handler) handleBulkExportFile(w http.ResponseWriter, r *http.Request, jobID, filename string) {
	if h.cfg.BulkExportService == nil {
		writeError(w, notImplementedEndpoint(r.URL.Path))
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r.Method, http.MethodGet)
		return
	}
	if err := h.authorizeExport(r.Context(), ""); err != nil {
		writeError(w, err)
		return
	}
	data, contentType, err := h.cfg.BulkExportService.OpenFile(r.Context(), jobID, filename)
	if err != nil {
		writeFileError(w, err, "export file not found")
		return
	}
	writeFileBody(w, data, contentType, "application/fhir+ndjson")
}

func parseCSVParam(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// prefersRespondAsync reports whether a Prefer header requests async processing.
// It accepts the FHIR Bulk Data token by itself or as one comma-separated
// preference, for example "respond-async, wait=10".
func prefersRespondAsync(header string) bool {
	for _, part := range strings.Split(header, ",") {
		token := strings.TrimSpace(part)
		if i := strings.IndexAny(token, "; "); i >= 0 {
			token = token[:i]
		}
		if strings.EqualFold(token, "respond-async") {
			return true
		}
	}
	return false
}

func notFound(message string, args ...any) error {
	return &core.ServiceError{
		Kind:    core.ErrorKindNotFound,
		Message: fmt.Sprintf(message, args...),
	}
}

func writeFileError(w http.ResponseWriter, err error, missing string) {
	if errors.Is(err, binary.ErrNotFound) {
		writeError(w, notFound("%s", missing))
		return
	}
	if errors.Is(err, binary.ErrInvalidArgument) {
		writeError(w, invalidRequest(err.Error(), err))
		return
	}
	writeError(w, err)
}

func writeFileBody(w http.ResponseWriter, rc io.ReadCloser, contentType, fallback string) {
	defer func() { _ = rc.Close() }()
	if contentType == "" {
		contentType = fallback
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}
