package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestSubscriptionCRUD(t *testing.T) {
	var stored string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/Subscription"):
			body := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(body)
			stored = string(body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"resourceType":"Subscription","id":"sub-1","status":"requested"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/Subscription/sub-1"):
			_, _ = w.Write([]byte(`{"resourceType":"Subscription","id":"sub-1","status":"active"}`))
		case r.Method == http.MethodPut:
			_, _ = w.Write([]byte(`{"resourceType":"Subscription","id":"sub-1","status":"active"}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/Subscription"):
			_, _ = w.Write([]byte(`{"resourceType":"Bundle","type":"searchset","entry":[{"resource":{"resourceType":"Subscription","id":"sub-1"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	sub := mustSubEnvelope(t, `{"resourceType":"Subscription","status":"requested","criteria":"Patient?active=true","channel":{"type":"rest-hook","endpoint":"https://app.example/hook"}}`)

	created, err := c.Subscriptions().Create(context.Background(), sub)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID != "sub-1" {
		t.Fatalf("id: %s", created.ID)
	}
	if stored == "" {
		t.Fatal("expected stored body")
	}

	read, err := c.Subscriptions().Read(context.Background(), "sub-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if read.ID != "sub-1" {
		t.Fatalf("read id: %s", read.ID)
	}

	sub.ID = "sub-1"
	if _, err := c.Subscriptions().Update(context.Background(), sub); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := c.Subscriptions().Delete(context.Background(), "sub-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	result, err := c.Subscriptions().Search(context.Background(), map[string]string{"status": "active"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("entries: %d", len(result.Entries))
	}
}

func TestSubscriptionPollStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"resourceType":"SubscriptionStatus","status":"active"}`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	env, err := c.Subscriptions().PollStatus(context.Background(), "/fhir/Subscription/sub-1/$status")
	if err != nil {
		t.Fatalf("PollStatus: %v", err)
	}
	if env.ResourceType != "SubscriptionStatus" {
		t.Fatalf("type: %s", env.ResourceType)
	}
}

func TestTypedResourceHelpers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/Patient/"):
			_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"p1"}`))
		case strings.Contains(r.URL.Path, "/Observation/"):
			_, _ = w.Write([]byte(`{"resourceType":"Observation","id":"o1"}`))
		case strings.Contains(r.URL.Path, "/Encounter/"):
			_, _ = w.Write([]byte(`{"resourceType":"Encounter","id":"e1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	for _, tc := range []struct {
		name string
		read func(context.Context, string) (*types.ResourceEnvelope, error)
		id   string
	}{
		{"Patient", c.Patient().Read, "p1"},
		{"Observation", c.Observation().Read, "o1"},
		{"Encounter", c.Encounter().Read, "e1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, err := tc.read(context.Background(), tc.id)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if env.ResourceType != tc.name {
				t.Fatalf("type: %s", env.ResourceType)
			}
		})
	}
}

func mustSubEnvelope(t *testing.T, jsonStr string) *types.ResourceEnvelope {
	t.Helper()
	env, err := types.NewJSONCodec().ParseJSON("Subscription", []byte(jsonStr))
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	return env
}
