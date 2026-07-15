package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestSyncPushPullSerialization(t *testing.T) {
	var gotPush PushRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sync/push":
			if r.Method != http.MethodPost {
				http.Error(w, "method", http.StatusMethodNotAllowed)
				return
			}
			_ = json.NewDecoder(r.Body).Decode(&gotPush)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(PushResponse{
				Results: []sync.PushResult{{
					EventID:           "evt-1",
					State:             sync.AckAccepted,
					CanonicalSequence: 42,
				}},
			})
		case "/sync/pull":
			if r.URL.Query().Get("nodeId") != "node-a" || r.URL.Query().Get("tenantId") != "tenant-1" {
				http.Error(w, "bad query", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(PullResponse{
				Events: []sync.CanonicalEvent{{
					EventID:           "canonical:42",
					TenantID:          "tenant-1",
					ResourceType:      "Patient",
					ResourceID:        "p1",
					Operation:         sync.EventTypeResourceCreated,
					CanonicalSequence: 42,
					Status:            sync.CanonicalStatusAccepted,
				}},
				NextCursor: 42,
				HasMore:    false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	env, _ := types.NewJSONCodec().ParseJSON("Patient", []byte(`{"resourceType":"Patient","id":"p1"}`))
	event := sync.LocalEvent{
		EventID:      "evt-1",
		OriginNodeID: "node-a",
		TenantID:     "tenant-1",
		ResourceType: "Patient",
		ResourceID:   "p1",
		Operation:    sync.EventTypeResourceCreated,
		LocalVersion: "1",
		Status:       sync.LocalEventStatusPending,
		CreatedAt:    time.Now().UTC(),
		ResourceAfter: env,
	}
	pushResp, err := c.Sync().Push(context.Background(), ToPushRequest("node-a", "tenant-1", []sync.LocalEvent{event}))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if gotPush.NodeID != "node-a" || gotPush.TenantID != "tenant-1" {
		t.Fatalf("push metadata: %+v", gotPush)
	}
	if len(gotPush.Events) != 1 || gotPush.Events[0].EventID != "evt-1" {
		t.Fatalf("events: %+v", gotPush.Events)
	}
	results := FromPushResponse(pushResp)
	if len(results) != 1 || results[0].State != sync.AckAccepted {
		t.Fatalf("results: %+v", results)
	}

	pullResp, err := c.Sync().Pull(context.Background(), PullRequest{
		NodeID:   "node-a",
		TenantID: "tenant-1",
		After:    0,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	events := FromPullResponse(pullResp)
	if len(events) != 1 || events[0].CanonicalSequence != 42 {
		t.Fatalf("events: %+v", events)
	}
}

func TestSyncRequiresNodeAndTenant(t *testing.T) {
	c, _ := New(Config{BaseURL: "http://example.com"})
	_, err := c.Sync().Push(context.Background(), PushRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	_, err = c.Sync().Pull(context.Background(), PullRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}
