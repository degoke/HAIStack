package http

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/degoke/health-ai-stack/pkg/bulkimport"
)

// BulkImportService handles FHIR Bulk Data import kickoff, polling, cancellation,
// and error-artifact download.
type BulkImportService interface {
	Kickoff(ctx context.Context, req bulkimport.KickoffRequest) (*bulkimport.Job, error)
	GetJob(ctx context.Context, jobID string) (*bulkimport.Job, error)
	Cancel(ctx context.Context, jobID string) error
	Manifest(job *bulkimport.Job) *bulkimport.Manifest
	StatusURL(jobID string) string
	GetFile(ctx context.Context, jobID, filename string) ([]byte, string, error)
}

func (h *handler) handleBulkImport(w http.ResponseWriter, r *http.Request) {
	if h.cfg.BulkImportService == nil {
		writeError(w, notImplementedEndpoint(r.URL.Path))
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodPost)
		return
	}
	if err := h.authorizeImportWrite(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	if !prefersRespondAsync(r.Header.Get("Prefer")) {
		writeError(w, invalidRequest("Prefer: respond-async is required for bulk import kickoff", nil))
		return
	}
	body, err := readBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	req, err := bulkimport.ParseParametersKickoff(body)
	if err != nil {
		writeError(w, invalidRequest(err.Error(), err))
		return
	}
	req.RequestURL = r.Method + " " + r.URL.RequestURI()
	req.RequiresAccess = h.cfg.AuthChecker != nil
	if principal, tenant, ok := identityFromContext(r.Context()); ok {
		req.TenantID = tenant.TenantID
		req.PrincipalID = principal.ID
	}
	job, err := h.cfg.BulkImportService.Kickoff(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Location", h.cfg.BulkImportService.StatusURL(job.ID))
	w.WriteHeader(http.StatusAccepted)
}

func (h *handler) handleBulkImportStatus(w http.ResponseWriter, r *http.Request, jobID string) {
	if h.cfg.BulkImportService == nil {
		writeError(w, notImplementedEndpoint(r.URL.Path))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if err := h.authorizeRead(r.Context(), "Parameters", ""); err != nil {
			writeError(w, err)
			return
		}
		job, err := h.cfg.BulkImportService.GetJob(r.Context(), jobID)
		if err != nil {
			writeError(w, err)
			return
		}
		if job == nil {
			writeError(w, notFound("import job %q not found", jobID))
			return
		}
		switch job.Status {
		case bulkimport.StatusComplete:
			manifest := h.cfg.BulkImportService.Manifest(job)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(manifest)
		case bulkimport.StatusInProgress:
			if job.Progress != "" {
				w.Header().Set("X-Progress", job.Progress)
			}
			w.WriteHeader(http.StatusAccepted)
		case bulkimport.StatusCancelled:
			writeError(w, invalidRequest("import job cancelled", nil))
		default:
			writeError(w, invalidRequest(job.LastError, nil))
		}
	case http.MethodDelete:
		if err := h.authorizeImportWrite(r.Context()); err != nil {
			writeError(w, err)
			return
		}
		if err := h.cfg.BulkImportService.Cancel(r.Context(), jobID); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	default:
		writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodDelete)
	}
}

func (h *handler) handleBulkImportFile(w http.ResponseWriter, r *http.Request, jobID, filename string) {
	if h.cfg.BulkImportService == nil {
		writeError(w, notImplementedEndpoint(r.URL.Path))
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r.Method, http.MethodGet)
		return
	}
	if err := h.authorizeRead(r.Context(), "Binary", ""); err != nil {
		writeError(w, err)
		return
	}
	data, contentType, err := h.cfg.BulkImportService.GetFile(r.Context(), jobID, filename)
	if err != nil {
		writeError(w, notFound("import file not found"))
		return
	}
	if contentType == "" {
		contentType = "application/fhir+ndjson"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// authorizeImportWrite requires write access for system-level $import. SMART has
// no dedicated import scope; a read-only export token must not be able to write.
func (h *handler) authorizeImportWrite(ctx context.Context) error {
	return h.authorizeWrite(ctx, "operation", "Parameters", "")
}
