package subscriptions

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/google/uuid"
)

// DeliveryWorker executes subscription delivery jobs.
type DeliveryWorker struct {
	Subscriptions store.SubscriptionStore
	Deliveries    store.SubscriptionDeliveryStore
	Resources     store.ResourceStore
	History       store.HistoryStore
	Webhook       *WebhookDispatcher
	Local         *LocalDispatcher
	Now           func() time.Time
}

// HandleJob implements jobs.Handler for subscription delivery.
func (w *DeliveryWorker) HandleJob(ctx context.Context, job store.JobRecord) error {
	if w == nil {
		return fmt.Errorf("subscriptions: delivery worker is nil")
	}
	var payload DeliverPayload
	if len(job.Payload) > 0 {
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("subscriptions: decode delivery payload: %w", err)
		}
	}
	rec, err := w.loadSubscription(ctx, payload.SubscriptionID)
	if err != nil {
		return err
	}
	if rec.Status != store.SubscriptionStatusActive {
		return nil
	}
	if w.alreadyDelivered(ctx, payload.SubscriptionID, payload.EventSequence) {
		return nil
	}
	attempt := job.Attempts
	if attempt <= 0 {
		attempt = 1
	}
	deliveryID := uuid.NewString()
	now := w.now()
	record := DeliveryRecord{
		ID:             deliveryID,
		SubscriptionID: payload.SubscriptionID,
		EventSequence:  payload.EventSequence,
		Attempt:        attempt,
		Status:         store.DeliveryStatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := w.Deliveries.Append(ctx, toStoreDelivery(record)); err != nil {
		return err
	}

	var resourceJSON []byte
	if payload.Action != store.EventActionDelete {
		if res := w.loadEventResource(ctx, payload); res != nil {
			resourceJSON = res.JSON
		}
	}

	var dispatchErr error
	var responseStatus int
	var responseBody string
	switch rec.Channel.Type {
	case ChannelTypeWebhook:
		if w.Webhook == nil {
			dispatchErr = fmt.Errorf("%w: webhook dispatcher is not configured", ErrInvalidChannel)
			break
		}
		var resource *types.ResourceEnvelope
		if len(resourceJSON) > 0 {
			resource = &types.ResourceEnvelope{
				ResourceType: payload.ResourceType,
				ID:           payload.ResourceID,
				VersionID:    payload.VersionID,
				JSON:         resourceJSON,
			}
		}
		result, err := w.Webhook.Dispatch(ctx, *rec.Channel.Webhook, payload, resource)
		responseStatus = result.StatusCode
		responseBody = result.Body
		dispatchErr = err
	case ChannelTypeLocal:
		if w.Local == nil {
			dispatchErr = fmt.Errorf("%w: local dispatcher is not configured", ErrInvalidChannel)
			break
		}
		dispatchErr = w.Local.Dispatch(ctx, *rec.Channel.Local, payload, resourceJSON)
	default:
		dispatchErr = fmt.Errorf("%w: %q", ErrInvalidChannel, rec.Channel.Type)
	}

	record.UpdatedAt = w.now()
	if dispatchErr != nil {
		record.Status = store.DeliveryStatusFailed
		record.ErrorMessage = dispatchErr.Error()
		record.ResponseStatus = responseStatus
		record.ResponseBody = responseBody
		if err := w.Deliveries.Update(ctx, toStoreDelivery(record)); err != nil {
			return err
		}
		return dispatchErr
	}
	record.Status = store.DeliveryStatusSuccess
	record.ResponseStatus = responseStatus
	record.ResponseBody = responseBody
	return w.Deliveries.Update(ctx, toStoreDelivery(record))
}

func (w *DeliveryWorker) loadSubscription(ctx context.Context, id string) (SubscriptionRecord, error) {
	if w.Subscriptions == nil {
		return SubscriptionRecord{}, ErrNilStore
	}
	stored, err := w.Subscriptions.Get(ctx, id)
	if err != nil {
		return SubscriptionRecord{}, err
	}
	return fromStoreRecord(*stored)
}

func (w *DeliveryWorker) loadEventResource(ctx context.Context, payload DeliverPayload) *types.ResourceEnvelope {
	if w.History != nil {
		history, err := w.History.GetHistory(ctx, payload.ResourceType, payload.ResourceID)
		if err == nil {
			for _, ver := range history {
				if ver.VersionID == payload.VersionID && ver.Resource != nil {
					return ver.Resource
				}
			}
		}
	}
	if w.Resources != nil {
		res, err := w.Resources.Read(ctx, payload.ResourceType, payload.ResourceID)
		if err == nil && res != nil {
			return res
		}
	}
	return nil
}

func (w *DeliveryWorker) alreadyDelivered(ctx context.Context, subscriptionID string, eventSequence int64) bool {
	if w.Deliveries == nil {
		return false
	}
	rows, err := w.Deliveries.List(ctx, store.DeliveryListQuery{
		SubscriptionID: subscriptionID,
		EventSequence:  eventSequence,
	})
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.Status == store.DeliveryStatusSuccess {
			return true
		}
	}
	return false
}

func (w *DeliveryWorker) now() time.Time {
	if w != nil && w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

// DeliveryJobRunner claims and executes subscription delivery jobs.
type DeliveryJobRunner struct {
	Jobs        store.JobStore
	Worker      *DeliveryWorker
	MaxAttempts int
	Backoff     jobs.Backoff
	Now         func() time.Time
}

// RunOnce claims at most one pending delivery job and executes it.
func (r *DeliveryJobRunner) RunOnce(ctx context.Context) (processed bool, err error) {
	if r == nil || r.Jobs == nil || r.Worker == nil {
		return false, fmt.Errorf("subscriptions: delivery job runner is not configured")
	}
	runner := jobs.NewRunner(r.Jobs)
	runner.MaxAttempts = r.MaxAttempts
	if runner.MaxAttempts <= 0 {
		runner.MaxAttempts = defaultRetryPolicy().MaxAttempts
	}
	runner.Backoff = r.Backoff
	runner.Now = r.Now
	if err := runner.Register(jobs.TypeSubscriptionsDeliver, jobs.HandlerFunc(r.Worker.HandleJob)); err != nil {
		return false, err
	}
	return runner.RunOnce(ctx)
}
