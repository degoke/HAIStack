package runtime

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/client"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
)

// httpSyncHub adapts client.SyncClient to hasync.Hub for device sync over HTTP.
type httpSyncHub struct {
	sync   *client.SyncClient
	nodeID string
	tenant string
}

func newHTTPSyncHub(hubURL, nodeID, tenantID string) (hasync.Hub, error) {
	c, err := client.New(client.Config{BaseURL: hubURL})
	if err != nil {
		return nil, fmt.Errorf("runtime: sync hub client: %w", err)
	}
	return &httpSyncHub{
		sync:   c.Sync(),
		nodeID: nodeID,
		tenant: tenantID,
	}, nil
}

func (h *httpSyncHub) Push(ctx context.Context, events []hasync.LocalEvent) ([]hasync.PushResult, error) {
	resp, err := h.sync.Push(ctx, client.PushRequest{
		NodeID:   h.nodeID,
		TenantID: h.tenant,
		Events:   events,
	})
	if err != nil {
		return nil, err
	}
	return client.FromPushResponse(resp), nil
}

func (h *httpSyncHub) Pull(ctx context.Context, afterSequence int64, limit int) ([]hasync.CanonicalEvent, error) {
	resp, err := h.sync.Pull(ctx, client.PullRequest{
		NodeID:   h.nodeID,
		TenantID: h.tenant,
		After:    afterSequence,
		Limit:    limit,
	})
	if err != nil {
		return nil, err
	}
	return client.FromPullResponse(resp), nil
}
