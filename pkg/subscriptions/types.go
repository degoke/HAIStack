package subscriptions

import (
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// TriggerEvent describes which resource lifecycle action fires a subscription.
type TriggerEvent string

const (
	TriggerEventCreate TriggerEvent = "create"
	TriggerEventUpdate TriggerEvent = "update"
	TriggerEventDelete TriggerEvent = "delete"
)

// Trigger defines when a subscription should fire.
type Trigger struct {
	ResourceType   string       `json:"resourceType"`
	Event          TriggerEvent `json:"event"`
	ChangedFields  []string     `json:"changedFields,omitempty"`
	FilterFHIRPath string       `json:"filterFhirPath,omitempty"`
}

// ChannelType names a delivery transport.
type ChannelType string

const (
	ChannelTypeWebhook ChannelType = "webhook"
	ChannelTypeLocal   ChannelType = "local"
)

// PayloadMode controls webhook body encoding.
type PayloadMode string

const (
	PayloadModeResourceJSON PayloadMode = "resource-json"
	PayloadModeEventOnly    PayloadMode = "event-only"
)

// WebhookConfig configures HTTP delivery.
type WebhookConfig struct {
	URL         string            `json:"url"`
	Method      string            `json:"method,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Timeout     time.Duration     `json:"timeout,omitempty"`
	PayloadMode PayloadMode       `json:"payloadMode,omitempty"`
}

// LocalConfig configures in-process handler delivery.
type LocalConfig struct {
	HandlerName string         `json:"handlerName"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Channel defines how matched events are delivered.
type Channel struct {
	Type    ChannelType    `json:"type"`
	Webhook *WebhookConfig `json:"webhook,omitempty"`
	Local   *LocalConfig   `json:"local,omitempty"`
}

// RetryPolicy configures delivery retries via pkg/jobs.
type RetryPolicy struct {
	MaxAttempts int `json:"maxAttempts"`
}

// SubscriptionRecord is the public subscription definition shape.
type SubscriptionRecord struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Status      store.SubscriptionStatus `json:"status"`
	Trigger     Trigger                  `json:"trigger"`
	Channel     Channel                  `json:"channel"`
	RetryPolicy RetryPolicy              `json:"retryPolicy"`
	CreatedAt   time.Time                `json:"createdAt"`
	UpdatedAt   time.Time                `json:"updatedAt"`
}

// DeliveryRecord is the public delivery log shape.
type DeliveryRecord struct {
	ID             string               `json:"id"`
	SubscriptionID string               `json:"subscriptionId"`
	EventSequence  int64                `json:"eventSequence"`
	Attempt        int                  `json:"attempt"`
	Status         store.DeliveryStatus `json:"status"`
	ResponseStatus int                  `json:"responseStatus,omitempty"`
	ResponseBody   string               `json:"responseBody,omitempty"`
	ErrorMessage   string               `json:"errorMessage,omitempty"`
	CreatedAt      time.Time            `json:"createdAt"`
	UpdatedAt      time.Time            `json:"updatedAt"`
}

// DeliverPayload is the background job payload for subscription delivery.
type DeliverPayload struct {
	SubscriptionID string            `json:"subscriptionId"`
	EventSequence  int64             `json:"eventSequence"`
	ResourceType   string            `json:"resourceType"`
	ResourceID     string            `json:"resourceId"`
	VersionID      string            `json:"versionId"`
	Action         store.EventAction `json:"action"`
	Attempt        int               `json:"attempt,omitempty"`
}

// CursorName returns a stable processor checkpoint name for a scope.
func CursorName(scope string) string {
	if scope == "" {
		scope = "default"
	}
	return "subscriptions.processor." + scope
}

func defaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 5}
}

func (p RetryPolicy) effectiveMaxAttempts() int {
	if p.MaxAttempts <= 0 {
		return defaultRetryPolicy().MaxAttempts
	}
	return p.MaxAttempts
}
