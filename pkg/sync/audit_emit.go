package sync

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/audit"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// appendAudit writes a sync audit event through the shared audit library when
// a store.AuditStore is configured.
func appendAudit(ctx context.Context, sink store.AuditStore, ev audit.SyncEvent) {
	if sink == nil {
		return
	}
	_ = audit.LogSyncEvent(ctx, &audit.StoreAdapter{Store: sink}, ev)
}
