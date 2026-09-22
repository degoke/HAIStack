package terminology

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteProviderLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/CodeSystem/$lookup" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(`{"resourceType":"Parameters","parameter":[{"name":"result","valueBoolean":true},{"name":"display","valueString":"Female"}]}`))
	}))
	defer srv.Close()

	p, err := NewRemoteProvider(RemoteConfig{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Lookup(context.Background(), LookupRequest{System: "http://example.org", Code: "female"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Concept.Display != "Female" {
		t.Fatalf("lookup=%+v", got)
	}
}

func TestRemoteProviderSubsumes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/CodeSystem/$subsumes" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("codeA") != "parent" || r.URL.Query().Get("codeB") != "child" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(`{"resourceType":"Parameters","parameter":[{"name":"outcome","valueBoolean":true}]}`))
	}))
	defer srv.Close()

	p, err := NewRemoteProvider(RemoteConfig{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := p.Subsumes(context.Background(), SubsumesRequest{
		System: "http://example.org", BroadCode: "parent", NarrowCode: "child",
	})
	if err != nil || !ok {
		t.Fatalf("subsumes: ok=%v err=%v", ok, err)
	}
}

func TestRemoteProviderValidateCodeUnavailable(t *testing.T) {
	failures := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failures++
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p, err := NewRemoteProvider(RemoteConfig{
		BaseURL:          srv.URL,
		FailureThreshold: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.ValidateCode(context.Background(), ValidateCodeRequest{
		Coding: Coding{System: "http://example.org", Code: "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != UnavailableProvider {
		t.Fatalf("status=%s", result.Status)
	}
}
