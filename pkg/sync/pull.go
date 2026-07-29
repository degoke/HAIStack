package sync

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/audit"
	"github.com/google/uuid"
)

// Puller fetches canonical events and applies them locally.
type Puller struct {
	Config  Config
	Applier *Applier
}

// PullResultSummary aggregates one pull pass.
type PullResultSummary struct {
	Fetched int
	Applied int
	Skipped int
	Cursor  int64
}

// Pull fetches canonical events after the pull cursor and applies accepted events locally.
func (p *Puller) Pull(ctx context.Context) (*PullResultSummary, error) {
	cfg := p.Config.normalized()
	if cfg.Hub == nil {
		return nil, fmt.Errorf("hub is required")
	}

	afterSeq, err := readCursorPosition(ctx, cfg.Cursors, cfg.PullCursorName)
	if err != nil {
		return nil, err
	}

	events, err := cfg.Hub.Pull(ctx, afterSeq, cfg.PullBatchSize)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return &PullResultSummary{Cursor: afterSeq}, nil
	}

	applier := p.Applier
	if applier == nil {
		applier = &Applier{
			Resources: cfg.Resources,
			History:   cfg.History,
			Inbox:     cfg.Inbox,
			Search:    cfg.Search,
			Indexer:   cfg.SearchIndexer,
			Clock:     cfg.Clock,
		}
	}

	summary := &PullResultSummary{
		Fetched: len(events),
		Cursor:  afterSeq,
	}

	var lastAppliedSeq = afterSeq
	for _, event := range events {
		if event.Status != CanonicalStatusAccepted {
			if event.CanonicalSequence > lastAppliedSeq {
				lastAppliedSeq = event.CanonicalSequence
			}
			continue
		}

		applied, err := applier.ApplyCanonical(ctx, event)
		if err != nil {
			return summary, err
		}
		if applied {
			summary.Applied++
		} else {
			summary.Skipped++
		}

		appendAudit(ctx, cfg.Audit, audit.SyncEvent{
			ID:           uuid.NewString(),
			Timestamp:    cfg.Clock(),
			Actor:        cfg.NodeID,
			Tenant:       cfg.TenantID,
			Action:       AuditDevicePulled,
			ResourceType: event.ResourceType,
			ResourceID:   event.ResourceID,
			Outcome:      string(event.Status),
			Details: map[string]string{
				"canonicalSequence": formatSequence(event.CanonicalSequence),
			},
		})

		if event.CanonicalSequence > lastAppliedSeq {
			lastAppliedSeq = event.CanonicalSequence
		}
	}

	if lastAppliedSeq > afterSeq {
		if err := upsertCursor(ctx, cfg.Cursors, cfg.PullCursorName, lastAppliedSeq, cfg.Clock()); err != nil {
			return summary, err
		}
		summary.Cursor = lastAppliedSeq
	}

	return summary, nil
}
