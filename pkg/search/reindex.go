package search

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

const ReindexJobType = jobs.TypeReindex

// ReindexPayload is the background job payload for search reindexing.
type ReindexPayload struct {
	ResourceType           string `json:"resourceType,omitempty"`
	SearchParameterURL     string `json:"searchParameterUrl,omitempty"`
	SearchParameterVersion string `json:"searchParameterVersion,omitempty"`
}

// Reindexer rebuilds search index rows using registry-driven extraction.
type Reindexer interface {
	ReindexAll(ctx context.Context, resourceType string) error
	ReindexResource(ctx context.Context, resource *types.ResourceEnvelope) error
	HandleJob(ctx context.Context, job store.JobRecord) error
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
	if payload.SearchParameterURL != "" {
		return w.reindexForSearchParameter(ctx, payload)
	}
	return w.ReindexAll(ctx, payload.ResourceType)
}

func (w *ReindexWorker) reindexForSearchParameter(ctx context.Context, payload ReindexPayload) error {
	if w.Registry == nil {
		return fmt.Errorf("search: registry is required")
	}
	typesToReindex, err := w.resourceTypesForSearchParameter(payload)
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

func (w *ReindexWorker) resourceTypesForSearchParameter(payload ReindexPayload) ([]string, error) {
	if payload.ResourceType != "" {
		if w.Registry == nil || !w.Registry.IsResourceEnabled(payload.ResourceType) {
			return nil, ErrResourceTypeDisabled
		}
		return []string{payload.ResourceType}, nil
	}
	if payload.SearchParameterURL == "" {
		return w.resourceTypes("")
	}
	var out []string
	for _, rt := range w.Registry.EnabledResourceTypes() {
		params := w.Registry.SearchParametersFor(rt)
		for _, p := range params {
			if p.CanonicalURL == payload.SearchParameterURL {
				if payload.SearchParameterVersion != "" && p.Version != payload.SearchParameterVersion {
					continue
				}
				out = append(out, rt)
				break
			}
		}
	}
	if len(out) == 0 {
		return w.Registry.EnabledResourceTypes(), nil
	}
	return out, nil
}

// EnqueueReindex schedules a reindex job for one resource type or all types when resourceType is empty.
func EnqueueReindex(ctx context.Context, jobs store.JobStore, resourceType string) (string, error) {
	return enqueueReindex(ctx, jobs, ReindexPayload{ResourceType: resourceType})
}

// EnqueueSearchParameterReindex schedules a reindex job for one SearchParameter definition.
func EnqueueSearchParameterReindex(ctx context.Context, jobs store.JobStore, canonicalURL, version string, resourceTypes ...string) (string, error) {
	payload := ReindexPayload{
		SearchParameterURL:     canonicalURL,
		SearchParameterVersion: version,
	}
	if len(resourceTypes) == 1 {
		payload.ResourceType = resourceTypes[0]
	}
	return enqueueReindex(ctx, jobs, payload)
}

func enqueueReindex(ctx context.Context, jobStore store.JobStore, payload ReindexPayload) (string, error) {
	if jobStore == nil {
		return "", fmt.Errorf("search: job store is required")
	}
	job, err := jobs.Enqueue(ctx, jobStore, ReindexJobType, payload, jobs.EnqueueOptions{})
	if err != nil {
		return "", err
	}
	return job.ID, nil
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
	liveIDs := make(map[string]struct{})
	offset := 0
	for {
		ids, err := w.Resources.ListIDs(ctx, resourceType, batch, offset)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			liveIDs[id] = struct{}{}
			if err := w.reindexOne(ctx, resourceType, id); err != nil {
				return err
			}
		}
		if len(ids) < batch {
			break
		}
		offset += len(ids)
	}
	return w.purgeOrphanedIndexRows(ctx, resourceType, liveIDs)
}

func (w *ReindexWorker) purgeOrphanedIndexRows(ctx context.Context, resourceType string, liveIDs map[string]struct{}) error {
	maintainer, ok := w.Search.(store.SearchIndexMaintainer)
	if !ok {
		return nil
	}
	indexed, err := maintainer.ListIndexedResourceIDs(ctx, resourceType)
	if err != nil {
		return err
	}
	for _, id := range indexed {
		if _, ok := liveIDs[id]; ok {
			continue
		}
		if err := w.Search.RemoveIndex(ctx, resourceType, id); err != nil {
			return err
		}
	}
	return nil
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

// ScheduleSearchParameterReindex implements registry.SearchParameterReindexNotifier.
func (n *ReindexNotifier) ScheduleSearchParameterReindex(ctx context.Context, canonicalURL, version string, resourceTypes ...string) error {
	if n == nil || n.Jobs == nil {
		return fmt.Errorf("search: reindex notifier is not configured")
	}
	if _, err := EnqueueSearchParameterReindex(ctx, n.Jobs, canonicalURL, version, resourceTypes...); err != nil {
		return err
	}
	return nil
}

// ReindexJobRunner claims and executes background reindex jobs through the
// shared jobs.Runner runtime.
type ReindexJobRunner struct {
	Jobs   store.JobStore
	Worker Reindexer
	Now    func() time.Time
}

// RunOnce claims at most one pending reindex job and executes it.
func (r *ReindexJobRunner) RunOnce(ctx context.Context) (processed bool, err error) {
	if r == nil || r.Jobs == nil || r.Worker == nil {
		return false, fmt.Errorf("search: reindex job runner is not configured")
	}
	runner := jobs.NewRunner(r.Jobs)
	runner.MaxAttempts = 1
	runner.Now = r.Now
	if err := runner.Register(ReindexJobType, jobs.HandlerFunc(r.Worker.HandleJob)); err != nil {
		return false, err
	}
	return runner.RunOnce(ctx)
}
