package subscriptions

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/google/uuid"
)

// Manager registers and manages subscription definitions.
type Manager struct {
	Store store.SubscriptionStore
	Now   func() time.Time
}

// Register creates a new active subscription.
func (m *Manager) Register(ctx context.Context, name string, trigger Trigger, channel Channel, retry RetryPolicy) (SubscriptionRecord, error) {
	if m == nil || m.Store == nil {
		return SubscriptionRecord{}, ErrNilStore
	}
	if err := validateTrigger(trigger); err != nil {
		return SubscriptionRecord{}, err
	}
	if err := validateChannel(channel); err != nil {
		return SubscriptionRecord{}, err
	}
	if retry.MaxAttempts <= 0 {
		retry = defaultRetryPolicy()
	}
	now := m.now()
	rec := SubscriptionRecord{
		ID:          uuid.NewString(),
		Name:        name,
		Status:      store.SubscriptionStatusActive,
		Trigger:     trigger,
		Channel:     channel,
		RetryPolicy: retry,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	stored, err := toStoreRecord(rec)
	if err != nil {
		return SubscriptionRecord{}, err
	}
	if err := m.Store.Create(ctx, stored); err != nil {
		return SubscriptionRecord{}, err
	}
	return rec, nil
}

// Update replaces an existing subscription definition.
func (m *Manager) Update(ctx context.Context, rec SubscriptionRecord) (SubscriptionRecord, error) {
	if m == nil || m.Store == nil {
		return SubscriptionRecord{}, ErrNilStore
	}
	if rec.ID == "" {
		return SubscriptionRecord{}, fmt.Errorf("%w: id is required", ErrNotFound)
	}
	if err := validateTrigger(rec.Trigger); err != nil {
		return SubscriptionRecord{}, err
	}
	if err := validateChannel(rec.Channel); err != nil {
		return SubscriptionRecord{}, err
	}
	existing, err := m.Store.Get(ctx, rec.ID)
	if err != nil {
		return SubscriptionRecord{}, err
	}
	if rec.RetryPolicy.MaxAttempts <= 0 {
		rec.RetryPolicy = defaultRetryPolicy()
	}
	rec.CreatedAt = existing.CreatedAt
	rec.UpdatedAt = m.now()
	if rec.Status == "" {
		rec.Status = existing.Status
	}
	stored, err := toStoreRecord(rec)
	if err != nil {
		return SubscriptionRecord{}, err
	}
	if err := m.Store.Update(ctx, stored); err != nil {
		return SubscriptionRecord{}, err
	}
	return rec, nil
}

// Disable marks a subscription inactive without deleting it.
func (m *Manager) Disable(ctx context.Context, id string) error {
	rec, err := m.Get(ctx, id)
	if err != nil {
		return err
	}
	rec.Status = store.SubscriptionStatusDisabled
	rec.UpdatedAt = m.now()
	stored, err := toStoreRecord(rec)
	if err != nil {
		return err
	}
	return m.Store.Update(ctx, stored)
}

// Get returns one subscription by id.
func (m *Manager) Get(ctx context.Context, id string) (SubscriptionRecord, error) {
	if m == nil || m.Store == nil {
		return SubscriptionRecord{}, ErrNilStore
	}
	stored, err := m.Store.Get(ctx, id)
	if err != nil {
		return SubscriptionRecord{}, err
	}
	return fromStoreRecord(*stored)
}

// List returns subscriptions matching optional filters.
func (m *Manager) List(ctx context.Context, status store.SubscriptionStatus, resourceType string, event TriggerEvent) ([]SubscriptionRecord, error) {
	if m == nil || m.Store == nil {
		return nil, ErrNilStore
	}
	query := store.SubscriptionListQuery{
		Status:       status,
		ResourceType: resourceType,
		Limit:        0,
	}
	if event != "" {
		query.EventKind = string(event)
	}
	stored, err := m.Store.List(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]SubscriptionRecord, 0, len(stored))
	for _, row := range stored {
		rec, err := fromStoreRecord(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// Delete removes a subscription definition.
func (m *Manager) Delete(ctx context.Context, id string) error {
	if m == nil || m.Store == nil {
		return ErrNilStore
	}
	return m.Store.Delete(ctx, id)
}

func (m *Manager) now() time.Time {
	if m != nil && m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}
