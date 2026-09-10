package analytics

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
)

const cdcCursorPrefix = "analytics.cdc."

// CDCProcessor consumes resource change events and enqueues analytics refresh jobs.
type CDCProcessor struct {
	Events    store.EventStore
	Cursors   store.CursorStore
	Jobs      store.JobStore
	Views     []string
	Scope     string
	BatchSize int
	Watermark *WatermarkStore
	Now       func() time.Time
}

// RunOnce reads one batch of events since the stored cursor and schedules refreshes.
func (p *CDCProcessor) RunOnce(ctx context.Context) (processed int, err error) {
	if p == nil || p.Events == nil || p.Cursors == nil || p.Jobs == nil {
		return 0, fmt.Errorf("analytics: cdc processor is not configured")
	}
	cursorName := cdcCursorName(p.Scope)
	position, err := p.loadCursor(ctx, cursorName)
	if err != nil {
		return 0, err
	}
	batch := p.BatchSize
	if batch <= 0 {
		batch = 100
	}
	events, err := p.Events.ReadSince(ctx, position, batch)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}
	touched := make(map[string]struct{})
	lastSeq := position
	for _, event := range events {
		touched[event.ResourceType] = struct{}{}
		lastSeq = event.Sequence
		processed++
	}
	for viewName := range viewsForResourceTypes(p.Views, touched) {
		if err := enqueueRefresh(ctx, p.Jobs, RefreshPayload{
			ViewName: viewName,
			Version:  "1.0.0",
			Actor:    "analytics-cdc",
		}); err != nil {
			return processed, err
		}
		if p.Watermark != nil {
			if err := p.Watermark.Advance(ctx, viewName, "1.0.0", p.now()); err != nil {
				return processed, err
			}
		}
	}
	return processed, p.Cursors.UpsertCursor(ctx, store.Cursor{
		Name:      cursorName,
		Position:  strconv.FormatInt(lastSeq, 10),
		UpdatedAt: p.now(),
	})
}

func viewsForResourceTypes(views []string, touched map[string]struct{}) map[string]struct{} {
	scheduled := make(map[string]struct{})
	for _, viewName := range views {
		if !IsSupportedView(viewName) {
			continue
		}
		resourceType := sourceResourceType(viewName)
		if _, ok := touched[resourceType]; ok {
			scheduled[viewName] = struct{}{}
		}
	}
	return scheduled
}

func sourceResourceType(viewName string) string {
	switch viewName {
	case ViewAppointment:
		return "Appointment"
	case ViewObservation:
		return "Observation"
	default:
		return "Patient"
	}
}

func cdcCursorName(scope string) string {
	if scope == "" {
		scope = "default"
	}
	return cdcCursorPrefix + scope
}

func (p *CDCProcessor) loadCursor(ctx context.Context, name string) (int64, error) {
	cursor, err := p.Cursors.GetCursor(ctx, name)
	if err != nil || cursor == nil || cursor.Position == "" {
		return 0, nil
	}
	pos, err := strconv.ParseInt(cursor.Position, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("analytics: parse cdc cursor %q: %w", cursor.Position, err)
	}
	return pos, nil
}

func (p *CDCProcessor) now() time.Time {
	if p != nil && p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

func enqueueRefresh(ctx context.Context, jobStore store.JobStore, payload RefreshPayload) error {
	jobID := refreshJobID(payload.ViewName, payload.Version)
	if existing, err := jobStore.Get(ctx, jobID); err == nil && existing != nil {
		if existing.Status == store.JobStatusPending || existing.Status == store.JobStatusRunning {
			return nil
		}
	}
	_, err := jobs.Enqueue(ctx, jobStore, TypeRefresh, payload, jobs.EnqueueOptions{ID: jobID})
	return err
}

func refreshJobID(viewName, version string) string {
	if version == "" {
		version = "1.0.0"
	}
	return "analytics-refresh-" + viewName + "-" + version
}
