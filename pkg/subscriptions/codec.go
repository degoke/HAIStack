package subscriptions

import (
	"encoding/json"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
)

func toStoreRecord(rec SubscriptionRecord) (store.SubscriptionRecord, error) {
	triggerJSON, err := json.Marshal(rec.Trigger)
	if err != nil {
		return store.SubscriptionRecord{}, fmt.Errorf("marshal trigger: %w", err)
	}
	channelJSON, err := json.Marshal(rec.Channel)
	if err != nil {
		return store.SubscriptionRecord{}, fmt.Errorf("marshal channel: %w", err)
	}
	retryJSON, err := json.Marshal(rec.RetryPolicy)
	if err != nil {
		return store.SubscriptionRecord{}, fmt.Errorf("marshal retry policy: %w", err)
	}
	return store.SubscriptionRecord{
		ID:           rec.ID,
		Name:         rec.Name,
		Status:       rec.Status,
		ResourceType: rec.Trigger.ResourceType,
		EventKind:    string(rec.Trigger.Event),
		TriggerJSON:  triggerJSON,
		ChannelJSON:  channelJSON,
		RetryJSON:    retryJSON,
		CreatedAt:    rec.CreatedAt.UTC(),
		UpdatedAt:    rec.UpdatedAt.UTC(),
	}, nil
}

func fromStoreRecord(rec store.SubscriptionRecord) (SubscriptionRecord, error) {
	var trigger Trigger
	if len(rec.TriggerJSON) > 0 {
		if err := json.Unmarshal(rec.TriggerJSON, &trigger); err != nil {
			return SubscriptionRecord{}, fmt.Errorf("unmarshal trigger: %w", err)
		}
	}
	var channel Channel
	if len(rec.ChannelJSON) > 0 {
		if err := json.Unmarshal(rec.ChannelJSON, &channel); err != nil {
			return SubscriptionRecord{}, fmt.Errorf("unmarshal channel: %w", err)
		}
	}
	var retry RetryPolicy
	if len(rec.RetryJSON) > 0 {
		if err := json.Unmarshal(rec.RetryJSON, &retry); err != nil {
			return SubscriptionRecord{}, fmt.Errorf("unmarshal retry policy: %w", err)
		}
	}
	return SubscriptionRecord{
		ID:          rec.ID,
		Name:        rec.Name,
		Status:      rec.Status,
		Trigger:     trigger,
		Channel:     channel,
		RetryPolicy: retry,
		CreatedAt:   rec.CreatedAt,
		UpdatedAt:   rec.UpdatedAt,
	}, nil
}

func fromStoreDelivery(rec store.DeliveryRecord) DeliveryRecord {
	return DeliveryRecord{
		ID:             rec.ID,
		SubscriptionID: rec.SubscriptionID,
		EventSequence:  rec.EventSequence,
		Attempt:        rec.Attempt,
		Status:         rec.Status,
		ResponseStatus: rec.ResponseStatus,
		ResponseBody:   rec.ResponseBody,
		ErrorMessage:   rec.ErrorMessage,
		CreatedAt:      rec.CreatedAt,
		UpdatedAt:      rec.UpdatedAt,
	}
}

func toStoreDelivery(rec DeliveryRecord) store.DeliveryRecord {
	return store.DeliveryRecord{
		ID:             rec.ID,
		SubscriptionID: rec.SubscriptionID,
		EventSequence:  rec.EventSequence,
		Attempt:        rec.Attempt,
		Status:         rec.Status,
		ResponseStatus: rec.ResponseStatus,
		ResponseBody:   rec.ResponseBody,
		ErrorMessage:   rec.ErrorMessage,
		CreatedAt:      rec.CreatedAt.UTC(),
		UpdatedAt:      rec.UpdatedAt.UTC(),
	}
}

func validateTrigger(t Trigger) error {
	if t.ResourceType == "" {
		return fmt.Errorf("%w: resource type is required", ErrInvalidTrigger)
	}
	switch t.Event {
	case TriggerEventCreate, TriggerEventUpdate, TriggerEventDelete:
	default:
		return fmt.Errorf("%w: unsupported event %q", ErrInvalidTrigger, t.Event)
	}
	if t.Event == TriggerEventUpdate && len(t.ChangedFields) == 0 && t.FilterFHIRPath == "" {
		// update without field filter matches any update on resource type
	}
	return nil
}

func validateChannel(ch Channel) error {
	switch ch.Type {
	case ChannelTypeWebhook:
		if ch.Webhook == nil || ch.Webhook.URL == "" {
			return fmt.Errorf("%w: webhook url is required", ErrInvalidChannel)
		}
	case ChannelTypeLocal:
		if ch.Local == nil || ch.Local.HandlerName == "" {
			return fmt.Errorf("%w: local handler name is required", ErrInvalidChannel)
		}
	default:
		return fmt.Errorf("%w: unsupported channel type %q", ErrInvalidChannel, ch.Type)
	}
	return nil
}

func eventActionMatches(trigger TriggerEvent, action store.EventAction) bool {
	return string(trigger) == string(action)
}

func deliveryJobID(subscriptionID string, eventSequence int64) string {
	return fmt.Sprintf("subscriptions:deliver:%s:%d", subscriptionID, eventSequence)
}

// DeliveryJobID returns the deterministic delivery job id for tests and tooling.
func DeliveryJobID(subscriptionID string, eventSequence int64) string {
	return deliveryJobID(subscriptionID, eventSequence)
}
