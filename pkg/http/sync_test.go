package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
)

type stubSyncHub struct {
	pullLimits *[]int
}

func (stubSyncHub) Push(_ context.Context, events []hasync.LocalEvent) ([]hasync.PushResult, error) {
	return []hasync.PushResult{{EventID: "e1", State: hasync.AckAccepted}}, nil
}

func (s stubSyncHub) Pull(_ context.Context, after int64, limit int) ([]hasync.CanonicalEvent, error) {
	if s.pullLimits != nil {
		*s.pullLimits = append(*s.pullLimits, limit)
	}
	return []hasync.CanonicalEvent{{CanonicalSequence: after + 1, TenantID: "tenant-1"}}, nil
}

func TestSyncHandlerPushPull(t *testing.T) {
	var pullLimits []int
	root := hahttp.NewRootHandler(nil, stubSyncHub{pullLimits: &pullLimits})

	pushBody := []byte(`{"nodeId":"node-1","tenantId":"tenant-1","events":[]}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/sync/push", bytes.NewReader(pushBody))
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("push status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var pushResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &pushResp); err != nil {
		t.Fatalf("unmarshal push: %v", err)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/sync/pull?nodeId=node-1&tenantId=tenant-1&after=0", nil)
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pull status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(pullLimits) != 1 || pullLimits[0] != 100 {
		t.Fatalf("default pull limit = %v, want [100]", pullLimits)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/sync/pull?nodeId=node-1&tenantId=tenant-1&after=0&limit=0", nil)
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("zero limit status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/sync/pull?nodeId=node-1&tenantId=tenant-1&after=-1", nil)
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative cursor status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
