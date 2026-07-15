package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestBulkExportKickoff(t *testing.T) {
	var statusBase string
	var gotRawQuery, gotAuth, gotUserAgent, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fhir/$export" {
			http.NotFound(w, r)
			return
		}
		gotRawQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		gotUserAgent = r.Header.Get("User-Agent")
		gotHeader = r.Header.Get("X-Test-Header")
		if r.Header.Get("Prefer") != "respond-async" {
			http.Error(w, "missing prefer", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Location", statusBase+"/export/status/1")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	statusBase = srv.URL

	c, _ := New(Config{
		BaseURL:       srv.URL,
		TokenProvider: StaticTokenProvider{Token: "secret"},
		DefaultHeaders: map[string]string{
			"X-Test-Header": "bulk-export",
		},
		UserAgent: "haistack-client-test",
	})
	job, err := c.BulkExport().Kickoff(context.Background(), ExportKickoffRequest{
		ResourceTypes: []string{"Patient", "Observation"},
		Since:         time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		OutputFormat:  "application/fhir+ndjson",
		TypeFilter:    "Observation?code=http://loinc.org|1234-5",
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.Status != ExportStatusInProgress || job.StatusURL == "" {
		t.Fatalf("job: %+v", job)
	}
	values, err := url.ParseQuery(gotRawQuery)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	if values.Get("_type") != "Patient,Observation" {
		t.Fatalf("_type: %q", values.Get("_type"))
	}
	if values.Get("_outputFormat") != "application/fhir+ndjson" {
		t.Fatalf("_outputFormat: %q", values.Get("_outputFormat"))
	}
	if values.Get("_typeFilter") != "Observation?code=http://loinc.org|1234-5" {
		t.Fatalf("_typeFilter: %q", values.Get("_typeFilter"))
	}
	if gotAuth != "Bearer secret" || gotUserAgent != "haistack-client-test" || gotHeader != "bulk-export" {
		t.Fatalf("headers: auth=%q user-agent=%q x-test-header=%q", gotAuth, gotUserAgent, gotHeader)
	}
}

func TestBulkExportPollAndManifest(t *testing.T) {
	var statusPath string
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/export/status/1":
			polls++
			if polls == 1 {
				w.Header().Set("X-Progress", "50%")
				w.WriteHeader(http.StatusAccepted)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"transactionTime":"2024-01-02T00:00:00Z","request":"GET /$export","output":[{"type":"Patient","url":"http://files/patient.ndjson"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/fhir/$export":
			w.Header().Set("Content-Location", statusPath)
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	statusPath = srv.URL + "/export/status/1"

	c, _ := New(Config{BaseURL: srv.URL})
	job, err := c.BulkExport().Kickoff(context.Background(), ExportKickoffRequest{})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}

	inProgress, err := c.BulkExport().PollStatus(context.Background(), job.StatusURL)
	if err != nil {
		t.Fatalf("PollStatus in-progress: %v", err)
	}
	if inProgress.Status != ExportStatusInProgress || inProgress.Progress != "50%" {
		t.Fatalf("progress: %+v", inProgress)
	}

	manifest, err := c.BulkExport().GetManifest(context.Background(), job.StatusURL)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if len(manifest.Output) != 1 || manifest.Output[0].Type != "Patient" {
		t.Fatalf("manifest: %+v", manifest)
	}
}

func TestBulkExportCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	if err := c.BulkExport().Cancel(context.Background(), srv.URL+"/export/status/1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
}

func TestBulkExportWaitTerminal(t *testing.T) {
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls++
		if polls < 2 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	job, err := c.BulkExport().Wait(ctx, srv.URL+"/status", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if job.Status != ExportStatusComplete {
		t.Fatalf("status: %s", job.Status)
	}
}
