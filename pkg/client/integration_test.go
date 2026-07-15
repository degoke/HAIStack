package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/client"
	"github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestIntegrationFHIRCRUDSearchPagination(t *testing.T) {
	patients := map[string][]byte{}
	searchPage := 0

	mux := http.NewServeMux()
	mux.HandleFunc("/fhir/metadata", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"resourceType":"CapabilityStatement","fhirVersion":"4.0.1","rest":[{"resource":[{"type":"Patient","interaction":[{"code":"read"},{"code":"create"},{"code":"search-type"}]}]}]}`))
	})
	mux.HandleFunc("/fhir/Patient", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var obj map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&obj)
			obj["id"] = "p-new"
			obj["meta"] = map[string]interface{}{"versionId": "1"}
			data, _ := json.Marshal(obj)
			patients["p-new"] = data
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(data)
			return
		}
		if r.Method == http.MethodGet {
			searchPage++
			if searchPage == 1 {
				_, _ = w.Write([]byte(`{"resourceType":"Bundle","type":"searchset","link":[{"relation":"next","url":"/fhir/Patient?page=2"}],"entry":[{"resource":{"resourceType":"Patient","id":"p1"}}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"resourceType":"Bundle","type":"searchset","entry":[{"resource":{"resourceType":"Patient","id":"p2"}}]}`))
		}
	})
	mux.HandleFunc("/fhir/Patient/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/fhir/Patient/")
		switch r.Method {
		case http.MethodGet:
			if data, ok := patients[id]; ok {
				_, _ = w.Write(data)
				return
			}
			_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"` + id + `"}`))
		case http.MethodPut:
			body, _ := ioReadAll(r)
			patients[id] = body
			_, _ = w.Write(body)
		case http.MethodDelete:
			delete(patients, id)
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("/sync/push", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.PushResponse{
			Results: []sync.PushResult{{EventID: "e1", State: sync.AckAccepted}},
		})
	})
	mux.HandleFunc("/sync/pull", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.PullResponse{NextCursor: 1})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := client.New(client.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	version, err := c.CheckFHIRVersion(context.Background())
	if err != nil {
		t.Fatalf("CheckFHIRVersion: %v", err)
	}
	if version != "4.0.1" {
		t.Fatalf("version: %s", version)
	}

	env, _ := types.NewJSONCodec().ParseJSON("Patient", []byte(`{"resourceType":"Patient","name":[{"family":"Test"}]}`))
	created, err := c.Create(context.Background(), env)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	read, err := c.Read(context.Background(), "Patient", created.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if read.ID != created.ID {
		t.Fatalf("read mismatch")
	}

	all, err := c.SearchAll(context.Background(), "Patient", nil)
	if err != nil {
		t.Fatalf("SearchAll: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("search all: %d", len(all))
	}

	_, err = c.Sync().Push(context.Background(), client.PushRequest{
		NodeID: "n1", TenantID: "t1",
		Events: []sync.LocalEvent{{EventID: "e1", TenantID: "t1", OriginNodeID: "n1", Operation: sync.EventTypeResourceCreated, CreatedAt: time.Now().UTC()}},
	})
	if err != nil {
		t.Fatalf("Sync Push: %v", err)
	}
}

func ioReadAll(r *http.Request) ([]byte, error) {
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := r.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf, nil
}
