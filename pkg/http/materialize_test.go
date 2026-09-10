package http_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/view"
)

type fakeMaterializeService struct {
	last view.MaterializeRequest
}

func (f *fakeMaterializeService) Kickoff(_ context.Context, req view.MaterializeRequest) (*view.MaterializeJob, error) {
	f.last = req
	return &view.MaterializeJob{
		ID:     "mat-job-1",
		Status: view.MaterializeComplete,
		Request: req,
	}, nil
}

func (f *fakeMaterializeService) GetJob(_ context.Context, jobID string) (*view.MaterializeJob, error) {
	return &view.MaterializeJob{
		ID:     jobID,
		Status: view.MaterializeComplete,
		Request: view.MaterializeRequest{
			ViewName: "patient_summary_view",
			Version:  "1.0.0",
		},
	}, nil
}

func (f *fakeMaterializeService) Cancel(context.Context, string) error { return nil }

func (f *fakeMaterializeService) StatusURL(jobID string) string {
	return "/fhir/ViewDefinition/$materialize/status/" + jobID
}

func (f *fakeMaterializeService) Result(job *view.MaterializeJob) *view.MaterializeResult {
	return &view.MaterializeResult{
		ViewName: job.Request.ViewName,
		Version:  job.Request.Version,
		RowCount: 1,
	}
}

func TestViewMaterializeKickoffAndStatus(t *testing.T) {
	matSvc := &fakeMaterializeService{}
	h := viewOpsHandler(t, hahttp.Config{ViewMaterializeService: matSvc})
	rec := doRequestWithHeaders(t, h, http.MethodPost,
		"/fhir/ViewDefinition/$materialize?viewName=patient_summary_view",
		nil, map[string]string{"Prefer": "respond-async"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("kickoff status=%d body=%s", rec.Code, rec.Body.String())
	}
	statusURL := rec.Header().Get("Content-Location")
	if !strings.Contains(statusURL, "/ViewDefinition/$materialize/status/mat-job-1") {
		t.Fatalf("Content-Location=%q", statusURL)
	}
	if matSvc.last.ViewName != "patient_summary_view" {
		t.Fatalf("ViewName=%q", matSvc.last.ViewName)
	}

	rec = doRequest(t, h, http.MethodGet, statusURL, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status poll=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "patient_summary_view") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestViewMaterializeNotImplementedWithoutService(t *testing.T) {
	h := viewOpsHandler(t, hahttp.Config{})
	rec := doRequestWithHeaders(t, h, http.MethodPost,
		"/fhir/ViewDefinition/$materialize?viewName=patient_summary_view",
		nil, map[string]string{"Prefer": "respond-async"})
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
