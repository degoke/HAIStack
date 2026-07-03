package search

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/google/uuid"
)

const ReindexJobType = "reindex"

// ReindexPayload is the background job payload for search reindexing.
type ReindexPayload struct {
	ResourceType string `json:"resourceType,omitempty"`
}

// ReindexWorker rebuilds search index rows using the same registry-driven indexer as writes.
type ReindexWorker struct {
	Registry  Registry
	Indexer   Indexer
	Resources store.ResourceStore
	Search    store.SearchStore
	BatchSize int
}

// ReindexAll rebuilds search rows for one resource type or all enabled types when resourceType is empty.
func (w *ReindexWorker) ReindexAll(ctx context.Context, resourceType string) error {
	if w == nil {
		return fmt.Errorf("search: reindex worker is nil")
	}
	typesToReindex, err := w.resourceTypes(resourceType)
	if err != nil {
		return err
	}
	for _, rt := range typesToReindex {
		if err := w.reindexType(ctx, rt); err != nil {
			return err
		}
	}
	return nil
}

// HandleJob processes one background reindex job.
func (w *ReindexWorker) HandleJob(ctx context.Context, job store.JobRecord) error {
	var payload ReindexPayload
	if len(job.Payload) > 0 {
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("search: decode reindex payload: %w", err)
		}
	}
	return w.ReindexAll(ctx, payload.ResourceType)
}

// EnqueueReindex schedules a reindex job.
func EnqueueReindex(ctx context.Context, jobs store.JobStore, resourceType string) (string, error) {
	if jobs == nil {
		return "", fmt.Errorf("search: job store is required")
	}
	payload, err := json.Marshal(ReindexPayload{ResourceType: resourceType})
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	job := store.JobRecord{
		ID:        id,
		Type:      ReindexJobType,
		Payload:   payload,
		Status:    store.JobStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := jobs.Enqueue(ctx, job); err != nil {
		return "", err
	}
	return id, nil
}

func (w *ReindexWorker) resourceTypes(resourceType string) ([]string, error) {
	if resourceType != "" {
		if w.Registry == nil || !w.Registry.IsResourceEnabled(resourceType) {
			return nil, ErrResourceTypeDisabled
		}
		return []string{resourceType}, nil
	}
	if w.Registry == nil {
		return nil, fmt.Errorf("search: registry is required for full reindex")
	}
	types := w.Registry.EnabledResourceTypes()
	if len(types) == 0 {
		return nil, nil
	}
	return types, nil
}

func (w *ReindexWorker) reindexType(ctx context.Context, resourceType string) error {
	if w.Indexer == nil || w.Search == nil || w.Resources == nil {
		return fmt.Errorf("search: reindex worker dependencies are incomplete")
	}
	batch := w.BatchSize
	if batch <= 0 {
		batch = 100
	}
	offset := 0
	for {
		ids, err := w.Resources.ListIDs(ctx, resourceType, batch, offset)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		for _, id := range ids {
			if err := w.reindexOne(ctx, resourceType, id); err != nil {
				return err
			}
		}
		if len(ids) < batch {
			return nil
		}
		offset += len(ids)
	}
}

func (w *ReindexWorker) reindexOne(ctx context.Context, resourceType, id string) error {
	res, err := w.Resources.Read(ctx, resourceType, id)
	if err != nil {
		return fmt.Errorf("search: reindex read %s/%s: %w", resourceType, id, err)
	}
	entries, err := w.Indexer.Build(ctx, res)
	if err != nil {
		return fmt.Errorf("search: reindex build %s/%s: %w", resourceType, id, err)
	}
	if err := w.Search.RemoveIndex(ctx, resourceType, id); err != nil {
		return err
	}
	for _, entry := range entries {
		entry.ResourceType = resourceType
		entry.ID = id
		if err := w.Search.Index(ctx, entry); err != nil {
			return err
		}
	}
	return nil
}

// ReindexResource rebuilds search rows for one resource instance.
func (w *ReindexWorker) ReindexResource(ctx context.Context, resource *types.ResourceEnvelope) error {
	if resource == nil {
		return fmt.Errorf("search: resource envelope is nil")
	}
	return w.reindexOne(ctx, resource.ResourceType, resource.ID)
}

// ReindexNotifier enqueues background reindex jobs when search parameters change.
type ReindexNotifier struct {
	Jobs store.JobStore
}

// NewReindexNotifier constructs a notifier backed by JobStore.
func NewReindexNotifier(jobs store.JobStore) *ReindexNotifier {
	return &ReindexNotifier{Jobs: jobs}
}

// ScheduleReindex implements registry.SearchReindexNotifier.
func (n *ReindexNotifier) ScheduleReindex(ctx context.Context, resourceTypes ...string) error {
	if n == nil || n.Jobs == nil {
		return fmt.Errorf("search: reindex notifier is not configured")
	}
	seen := make(map[string]struct{}, len(resourceTypes))
	for _, resourceType := range resourceTypes {
		if resourceType == "" {
			continue
		}
		if _, ok := seen[resourceType]; ok {
			continue
		}
		seen[resourceType] = struct{}{}
		if _, err := EnqueueReindex(ctx, n.Jobs, resourceType); err != nil {
			return err
		}
	}
	return nil
}

// ReindexJobRunner claims and executes background reindex jobs.
type ReindexJobRunner struct {
	Jobs   store.JobStore
	Worker *ReindexWorker
	Now    func() time.Time
}

// RunOnce claims at most one pending reindex job and executes it.
func (r *ReindexJobRunner) RunOnce(ctx context.Context) (processed bool, err error) {
	if r == nil || r.Jobs == nil || r.Worker == nil {
		return false, fmt.Errorf("search: reindex job runner is not configured")
	}
	job, err := r.Jobs.ClaimNext(ctx, ReindexJobType)
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil
	}

	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now()
	}

	handleErr := r.Worker.HandleJob(ctx, *job)
	job.UpdatedAt = now
	if handleErr != nil {
		job.Status = store.JobStatusFailed
		job.LastError = handleErr.Error()
	} else {
		job.Status = store.JobStatusCompleted
		job.LastError = ""
	}
	if updateErr := r.Jobs.Update(ctx, *job); updateErr != nil {
		return true, updateErr
	}
	return true, handleErr
}
