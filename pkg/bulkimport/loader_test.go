package bulkimport_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/degoke/haistack/pkg/bulkimport"
)

func TestHTTPLoaderRejectsFileURL(t *testing.T) {
	loader := bulkimport.NewHTTPLoader()
	_, err := loader.Load(context.Background(), "file:///etc/passwd")
	if err == nil {
		t.Fatal("expected file:// to be rejected")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("error = %v, want scheme rejection", err)
	}
}

func TestHTTPLoaderRejectsUnexpectedSchemes(t *testing.T) {
	loader := bulkimport.NewHTTPLoader()
	for _, raw := range []string{"ftp://example.test/Patient.ndjson", "data:text/plain,hi", "/etc/passwd"} {
		if _, err := loader.Load(context.Background(), raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestHTTPLoaderFetchesHTTP(t *testing.T) {
	want := `{"resourceType":"Patient","id":"p1"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/fhir+ndjson")
		_, _ = io.WriteString(w, want)
	}))
	t.Cleanup(server.Close)

	loader := bulkimport.NewHTTPLoader()
	got, err := loader.Load(context.Background(), server.URL+"/Patient.ndjson")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(got) != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
