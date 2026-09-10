package conceptmap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteHTTPClientTranslate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ConceptMap/$translate" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(`{
			"resourceType": "Parameters",
			"parameter": [{
				"name": "match",
				"part": [{
					"name": "concept",
					"part": [{
						"name": "coding",
						"valueCoding": {
							"system": "http://target.example",
							"code": "mapped",
							"display": "Mapped"
						}
					}]
				}]
			}]
		}`))
	}))
	defer server.Close()

	client := RemoteHTTPClient{BaseURL: server.URL}
	codings, err := client.Translate(context.Background(), TranslateRequest{
		MapCanonical: "http://example.org/ConceptMap/test",
		Source:       map[string]any{"system": "http://source.example", "code": "src"},
		TargetSystem: "http://target.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(codings) != 1 || codings[0]["code"] != "mapped" {
		t.Fatalf("unexpected codings: %#v", codings)
	}
}

func TestTranslatorFallsBackToRemote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(`{
			"resourceType": "Parameters",
			"parameter": [{
				"name": "match",
				"part": [{
					"name": "concept",
					"valueCoding": {"system": "http://target.example", "code": "remote"}
				}]
			}]
		}`))
	}))
	defer server.Close()

	translator := Translator{
		Resolver: StaticResolver{},
		Remote:   RemoteHTTPClient{BaseURL: server.URL},
	}
	codings, err := translator.Translate(context.Background(), TranslateRequest{
		MapCanonical: "http://example.org/ConceptMap/missing",
		Source:       map[string]any{"code": "src"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(codings) != 1 || codings[0]["code"] != "remote" {
		t.Fatalf("unexpected codings: %#v", codings)
	}
}
