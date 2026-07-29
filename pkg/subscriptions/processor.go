package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// Processor consumes store.ResourceEvent entries and enqueues delivery jobs.
type Processor struct {
	Events        store.EventStore
	Cursors       store.CursorStore
	Subscriptions store.SubscriptionStore
	Jobs          store.JobStore
	Resources     store.ResourceStore
	History       store.HistoryStore
	Matcher       *Matcher
	Scope         string
	BatchSize     int
	Now           func() time.Time
}

// RunOnce reads one batch of events since the stored cursor and schedules deliveries.
func (p *Processor) RunOnce(ctx context.Context) (processed int, err error) {
	if p == nil || p.Events == nil || p.Cursors == nil || p.Subscriptions == nil || p.Jobs == nil {
		return 0, fmt.Errorf("subscriptions: processor is not configured")
	}
	if p.Matcher == nil {
		return 0, ErrNilEngine
	}
	cursorName := CursorName(p.Scope)
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
	lastSeq := position
	for _, event := range events {
		if err := p.processEvent(ctx, event); err != nil {
			return processed, err
		}
		processed++
		lastSeq = event.Sequence
	}
	if err := p.Cursors.UpsertCursor(ctx, store.Cursor{
		Name:      cursorName,
		Position:  strconv.FormatInt(lastSeq, 10),
		UpdatedAt: p.now(),
	}); err != nil {
		return processed, err
	}
	return processed, nil
}

// RunLoop repeatedly calls RunOnce until ctx is cancelled.
func (p *Processor) RunLoop(ctx context.Context, idle time.Duration) error {
	if idle <= 0 {
		idle = time.Second
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := p.RunOnce(ctx)
		if err != nil {
			return err
		}
		if n == 0 {
			timer := time.NewTimer(idle)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
	}
}

func (p *Processor) processEvent(ctx context.Context, event store.ResourceEvent) error {
	subs, err := p.Subscriptions.List(ctx, store.SubscriptionListQuery{
		Status:       store.SubscriptionStatusActive,
		ResourceType: event.ResourceType,
		EventKind:    string(event.Action),
	})
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return nil
	}
	current, previous, err := p.loadResources(ctx, event)
	if err != nil {
		return err
	}
	mc := MatchContext{Event: event, Current: current, Previous: previous}
	for _, stored := range subs {
		rec, err := fromStoreRecord(stored)
		if err != nil {
			return err
		}
		if rec.Status != store.SubscriptionStatusActive {
			continue
		}
		ok, err := p.Matcher.Matches(ctx, rec.Trigger, mc)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if err := enqueueDelivery(ctx, p.Jobs, DeliverPayload{
			SubscriptionID: rec.ID,
			EventSequence:  event.Sequence,
			ResourceType:   event.ResourceType,
			ResourceID:     event.ID,
			VersionID:      event.VersionID,
			Action:         event.Action,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *Processor) loadResources(ctx context.Context, event store.ResourceEvent) (*types.ResourceEnvelope, *types.ResourceEnvelope, error) {
	var (
		current  *types.ResourceEnvelope
		previous *types.ResourceEnvelope
	)

	if p.History != nil {
		history, err := p.History.GetHistory(ctx, event.ResourceType, event.ID)
		if err != nil {
			return nil, nil, err
		}
		matchIndex := -1
		for i, ver := range history {
			if ver.VersionID == event.VersionID {
				matchIndex = i
				if ver.Resource != nil {
					current = ver.Resource
				}
				break
			}
		}
		if matchIndex >= 0 {
			for i := matchIndex - 1; i >= 0; i-- {
				if history[i].Resource != nil {
					previous = history[i].Resource
					break
				}
			}
		}
	}

	if current == nil && p.Resources != nil && event.Action != store.EventActionDelete {
		res, err := p.Resources.Read(ctx, event.ResourceType, event.ID)
		if err == nil {
			current = res
		}
	}
	return current, previous, nil
}

func (p *Processor) loadCursor(ctx context.Context, name string) (int64, error) {
	cursor, err := p.Cursors.GetCursor(ctx, name)
	if err != nil || cursor == nil || cursor.Position == "" {
		return 0, nil
	}
	pos, err := strconv.ParseInt(cursor.Position, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("subscriptions: parse cursor %q: %w", cursor.Position, err)
	}
	return pos, nil
}

func (p *Processor) now() time.Time {
	if p != nil && p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

func enqueueDelivery(ctx context.Context, jobStore store.JobStore, payload DeliverPayload) error {
	if jobStore == nil {
		return ErrNilStore
	}
	jobID := deliveryJobID(payload.SubscriptionID, payload.EventSequence)
	if existing, err := jobStore.Get(ctx, jobID); err == nil && existing != nil {
		return nil
	}
	_, err := jobs.Enqueue(ctx, jobStore, jobs.TypeSubscriptionsDeliver, payload, jobs.EnqueueOptions{
		ID: jobID,
	})
	if errors.Is(err, jobs.ErrDuplicateJob) {
		return nil
	}
	return err
}
