package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/degoke/health-ai-stack/pkg/sync"
)

const (
	syncPushPath = "/sync/push"
	syncPullPath = "/sync/pull"
)

// SyncClient provides HAIStack sync push/pull over HTTP.
type SyncClient struct {
	client *Client
}

// PushRequest is the HTTP wire payload for POST /sync/push.
type PushRequest struct {
	NodeID   string            `json:"nodeId"`
	TenantID string            `json:"tenantId"`
	Events   []sync.LocalEvent `json:"events"`
}

// PushResponse is the HTTP wire response for POST /sync/push.
type PushResponse struct {
	Results []sync.PushResult `json:"results"`
}

// PullRequest carries pull cursor parameters.
type PullRequest struct {
	NodeID   string `json:"nodeId"`
	TenantID string `json:"tenantId"`
	After    int64  `json:"after"`
	Limit    int    `json:"limit,omitempty"`
}

// PullResponse is the HTTP wire response for GET /sync/pull.
type PullResponse struct {
	Events     []sync.CanonicalEvent `json:"events"`
	NextCursor int64                 `json:"nextCursor"`
	HasMore    bool                  `json:"hasMore"`
}

// Push submits local events to the sync hub.
func (s *SyncClient) Push(ctx context.Context, req PushRequest) (*PushResponse, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("sync client is nil")
	}
	if req.NodeID == "" {
		return nil, fmt.Errorf("nodeId is required")
	}
	if req.TenantID == "" {
		return nil, fmt.Errorf("tenantId is required")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	raw, err := s.client.do(ctx, requestOptions{
		method: "POST",
		url:    s.client.baseURL + syncPushPath,
		body:   body,
	})
	if err != nil {
		return nil, err
	}
	var resp PushResponse
	if err := json.Unmarshal(raw.Body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Pull fetches canonical events after a cursor.
func (s *SyncClient) Pull(ctx context.Context, req PullRequest) (*PullResponse, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("sync client is nil")
	}
	if req.NodeID == "" {
		return nil, fmt.Errorf("nodeId is required")
	}
	if req.TenantID == "" {
		return nil, fmt.Errorf("tenantId is required")
	}
	values := url.Values{}
	values.Set("nodeId", req.NodeID)
	values.Set("tenantId", req.TenantID)
	values.Set("after", strconv.FormatInt(req.After, 10))
	if req.Limit > 0 {
		values.Set("limit", strconv.Itoa(req.Limit))
	}
	u := s.client.baseURL + syncPullPath + "?" + values.Encode()
	raw, err := s.client.do(ctx, requestOptions{
		method: "GET",
		url:    u,
		accept: "application/json",
	})
	if err != nil {
		return nil, err
	}
	var resp PullResponse
	if err := json.Unmarshal(raw.Body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ToPushRequest converts local events into a push request with explicit node and tenant IDs.
func ToPushRequest(nodeID, tenantID string, events []sync.LocalEvent) PushRequest {
	return PushRequest{
		NodeID:   nodeID,
		TenantID: tenantID,
		Events:   events,
	}
}

// FromPushResponse returns push results compatible with pkg/sync.Engine expectations.
func FromPushResponse(resp *PushResponse) []sync.PushResult {
	if resp == nil {
		return nil
	}
	return resp.Results
}

// FromPullResponse returns canonical events from a pull response.
func FromPullResponse(resp *PullResponse) []sync.CanonicalEvent {
	if resp == nil {
		return nil
	}
	return resp.Events
}
