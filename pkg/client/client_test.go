package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestNewRequiresBaseURL(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("expected error for empty BaseURL")
	}
}

func TestCRUDRequestConstruction(t *testing.T) {
	var gotMethod, gotPath, gotContentType, gotAuth string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		gotBody = body

		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/Patient"):
			w.Header().Set("Content-Type", "application/fhir+json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"p1","meta":{"versionId":"1"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/Patient/p1"):
			w.Header().Set("Content-Type", "application/fhir+json")
			_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"p1","meta":{"versionId":"1"}}`))
		case r.Method == http.MethodPut:
			w.Header().Set("Content-Type", "application/fhir+json")
			_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"p1","meta":{"versionId":"2"}}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, err := New(Config{
		BaseURL:       srv.URL,
		TokenProvider: StaticTokenProvider{Token: "secret"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	patientJSON := `{"resourceType":"Patient","name":[{"family":"Smith"}]}`
	env, err := types.NewJSONCodec().ParseJSON("Patient", []byte(patientJSON))
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}

	created, err := c.Create(context.Background(), env)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if gotMethod != http.MethodPost || !strings.HasSuffix(gotPath, "/fhir/Patient") {
		t.Fatalf("create request: %s %s", gotMethod, gotPath)
	}
	if gotContentType != "application/fhir+json" {
		t.Fatalf("content type: %s", gotContentType)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("auth: %s", gotAuth)
	}
	if created.ID != "p1" {
		t.Fatalf("created id: %s", created.ID)
	}

	read, err := c.Read(context.Background(), "Patient", "p1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if read.ID != "p1" {
		t.Fatalf("read id: %s", read.ID)
	}

	env.ID = "p1"
	updated, err := c.Update(context.Background(), env)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.VersionID != "2" {
		t.Fatalf("version: %s", updated.VersionID)
	}
	_ = gotBody

	if err := c.Delete(context.Background(), "Patient", "p1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestErrorOperationOutcomeParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"not-found","diagnostics":"Patient/p99 not found"}]}`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	_, err := c.Read(context.Background(), "Patient", "p99")
	if err == nil {
		t.Fatal("expected error")
	}
	ce, ok := AsError(err)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if ce.StatusCode != http.StatusNotFound {
		t.Fatalf("status: %d", ce.StatusCode)
	}
	if ce.Outcome == nil || len(ce.Outcome.Issue) == 0 {
		t.Fatal("expected outcome")
	}
	if ce.Outcome.Issue[0].Code != "not-found" {
		t.Fatalf("code: %s", ce.Outcome.Issue[0].Code)
	}
	if ce.Retryable {
		t.Fatal("404 should not be retryable")
	}
}

func TestErrorRawNonFHIR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal failure"))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	_, err := c.Read(context.Background(), "Patient", "x")
	ce, ok := AsError(err)
	if !ok {
		t.Fatalf("expected *Error, got %v", err)
	}
	if ce.Outcome != nil {
		t.Fatal("expected no outcome for non-FHIR body")
	}
	if !ce.Retryable {
		t.Fatal("500 should be retryable")
	}
}

func TestRetryOnServerError(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"exception"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"ok"}`))
	}))
	defer srv.Close()

	c, _ := New(Config{
		BaseURL: srv.URL,
		RetryPolicy: &DefaultRetryPolicy{
			Attempts:     3,
			InitialDelay: 1 * time.Millisecond,
			MaxDelay:     5 * time.Millisecond,
		},
	})
	env, err := c.Read(context.Background(), "Patient", "ok")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if env.ID != "ok" {
		t.Fatalf("id: %s", env.ID)
	}
	if attempts != 2 {
		t.Fatalf("attempts: %d", attempts)
	}
}

func TestMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fhir/metadata" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"resourceType":"CapabilityStatement","fhirVersion":"4.0.1","format":["application/fhir+json"],"rest":[{"resource":[{"type":"Patient","interaction":[{"code":"read"},{"code":"search-type"}]}]}]}`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	meta, err := c.Metadata(context.Background())
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if meta.FHIRVersion != "4.0.1" {
		t.Fatalf("version: %s", meta.FHIRVersion)
	}
	supported, err := c.CheckFeatureSupport(context.Background(), "Patient", "search-type")
	if err != nil {
		t.Fatalf("CheckFeatureSupport: %v", err)
	}
	if !supported {
		t.Fatal("expected search-type support")
	}
}

func TestTransactionBundle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/fhir" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"resourceType":"Bundle","type":"transaction-response","entry":[]}`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	builder := NewTransactionBundleBuilder().
		CreateEntry(mustEnvelope(t, "Patient", `{"resourceType":"Patient","id":"new"}`))
	bundle, err := builder.Submit(context.Background(), c)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if bundle.ResourceType != "Bundle" {
		t.Fatalf("type: %s", bundle.ResourceType)
	}
}

func mustEnvelope(t *testing.T, rt, jsonStr string) *types.ResourceEnvelope {
	t.Helper()
	env, err := types.NewJSONCodec().ParseJSON(rt, []byte(jsonStr))
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	return env
}

func TestDecodeSearchBundle(t *testing.T) {
	data := []byte(`{"resourceType":"Bundle","type":"searchset","total":1,"link":[{"relation":"next","url":"http://example/fhir/Patient?page=2"}],"entry":[{"resource":{"resourceType":"Patient","id":"p1"}}]}`)
	result, err := parseSearchBundle(types.NewJSONCodec(), "Patient", data)
	if err != nil {
		t.Fatalf("parseSearchBundle: %v", err)
	}
	if result.Total == nil || *result.Total != 1 {
		t.Fatalf("total: %v", result.Total)
	}
	if result.NextURL == "" {
		t.Fatal("expected next url")
	}
	if len(result.Entries) != 1 || result.Entries[0].ID != "p1" {
		t.Fatalf("entries: %+v", result.Entries)
	}
}

func TestNormalizeFHIRVersion(t *testing.T) {
	if NormalizeFHIRVersion("4.0.1") != "R4" {
		t.Fatal("expected R4")
	}
}

func TestRawResponsePreservedInSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"resourceType":"Bundle","type":"searchset","entry":[]}`))
	}))
	defer srv.Close()
	c, _ := New(Config{BaseURL: srv.URL})
	result, err := c.Search(context.Background(), "Patient", nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(result.RawBundle, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}
