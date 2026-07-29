package store

import (
	"context"
	"time"
)

// SubscriptionStatus describes whether a subscription participates in matching.
type SubscriptionStatus string

const (
	SubscriptionStatusActive   SubscriptionStatus = "active"
	SubscriptionStatusDisabled SubscriptionStatus = "disabled"
)

// SubscriptionRecord is a persisted subscription definition.
type SubscriptionRecord struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Status       SubscriptionStatus `json:"status"`
	ResourceType string             `json:"resourceType"`
	EventKind    string             `json:"eventKind"`
	TriggerJSON  []byte             `json:"trigger"`
	ChannelJSON  []byte             `json:"channel"`
	RetryJSON    []byte             `json:"retryPolicy,omitempty"`
	CreatedAt    time.Time          `json:"createdAt"`
	UpdatedAt    time.Time          `json:"updatedAt"`
}

// SubscriptionListQuery filters subscription registry reads.
type SubscriptionListQuery struct {
	Status       SubscriptionStatus
	ResourceType string
	EventKind    string
	Limit        int
}

// DeliveryStatus describes one delivery attempt outcome.
type DeliveryStatus string

const (
	DeliveryStatusPending  DeliveryStatus = "pending"
	DeliveryStatusSuccess  DeliveryStatus = "success"
	DeliveryStatusFailed   DeliveryStatus = "failed"
	DeliveryStatusRetrying DeliveryStatus = "retrying"
)

// DeliveryRecord is an append-only delivery log entry.
type DeliveryRecord struct {
	ID             string         `json:"id"`
	SubscriptionID string         `json:"subscriptionId"`
	EventSequence  int64          `json:"eventSequence"`
	Attempt        int            `json:"attempt"`
	Status         DeliveryStatus `json:"status"`
	ResponseStatus int            `json:"responseStatus,omitempty"`
	ResponseBody   string         `json:"responseBody,omitempty"`
	ErrorMessage   string         `json:"errorMessage,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// DeliveryListQuery filters delivery log reads.
type DeliveryListQuery struct {
	SubscriptionID string
	EventSequence  int64
	Limit          int
}

// SubscriptionStore persists subscription definitions.
type SubscriptionStore interface {
	Create(ctx context.Context, record SubscriptionRecord) error
	Update(ctx context.Context, record SubscriptionRecord) error
	Get(ctx context.Context, id string) (*SubscriptionRecord, error)
	List(ctx context.Context, query SubscriptionListQuery) ([]SubscriptionRecord, error)
	Delete(ctx context.Context, id string) error
}

// SubscriptionDeliveryStore persists delivery attempt logs.
type SubscriptionDeliveryStore interface {
	Append(ctx context.Context, record DeliveryRecord) error
	Update(ctx context.Context, record DeliveryRecord) error
	Get(ctx context.Context, id string) (*DeliveryRecord, error)
	List(ctx context.Context, query DeliveryListQuery) ([]DeliveryRecord, error)
}
