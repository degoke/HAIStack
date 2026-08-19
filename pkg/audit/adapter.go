package audit

import (
	"context"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/google/uuid"
)

// StoreAdapter writes Event values to a store.AuditStore and can list them
// back as Event values.
type StoreAdapter struct {
	Store store.AuditStore
	Now   func() time.Time
	NewID func() string
}

// Log implements Logger.
func (a *StoreAdapter) Log(ctx context.Context, event Event) error {
	if a == nil || a.Store == nil {
		return ErrNilStore
	}
	rec := ToStoreRecord(a.normalize(event))
	return a.Store.Append(ctx, rec)
}

// List returns store records matching store.AuditQuery filters.
func (a *StoreAdapter) List(ctx context.Context, query store.AuditQuery) ([]store.AuditRecord, error) {
	if a == nil || a.Store == nil {
		return nil, ErrNilStore
	}
	return a.Store.List(ctx, query)
}

// ListEvents lists and converts records using store-level filters where supported.
func (a *StoreAdapter) ListEvents(ctx context.Context, query Query) ([]Event, error) {
	if a == nil || a.Store == nil {
		return nil, ErrNilStore
	}
	records, err := a.Store.List(ctx, store.AuditQuery{
		ResourceType:   query.ResourceType,
		ResourceID:     query.ResourceID,
		Actor:          query.Actor,
		Action:         query.Action,
		Outcome:        query.Outcome,
		Tenant:         query.Tenant,
		ViewName:       query.ViewName,
		ToolName:       query.ToolName,
		ConversationID: query.ConversationID,
		After:          query.After,
		Before:         query.Before,
		Limit:          query.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(records))
	for _, rec := range records {
		out = append(out, FromStoreRecord(rec))
	}
	return out, nil
}

func (a *StoreAdapter) normalize(event Event) Event {
	now := time.Now().UTC()
	if a.Now != nil {
		now = a.Now().UTC()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = now
	}
	if event.ID == "" {
		if a.NewID != nil {
			event.ID = a.NewID()
		} else {
			event.ID = "audit-" + uuid.NewString()
		}
	}
	return event
}

// ToStoreRecord converts an Event into store.AuditRecord using first-class fields.
// Legacy detail keys are still lifted on read via FromStoreRecord when columns are empty.
func ToStoreRecord(event Event) store.AuditRecord {
	return store.AuditRecord{
		ID:             event.ID,
		Timestamp:      event.Timestamp,
		Actor:          event.Actor,
		Action:         event.Action,
		ResourceType:   event.ResourceType,
		ResourceID:     event.ResourceID,
		Outcome:        event.Outcome,
		Tenant:         event.Tenant,
		Subject:        event.Subject,
		ViewName:       event.ViewName,
		ToolName:       event.ToolName,
		ConversationID: event.ConversationID,
		ModuleName:     event.ModuleName,
		BlobKey:        event.BlobKey,
		Details:        cloneDetails(event.Details),
	}
}

// FromStoreRecord converts a store.AuditRecord into Event, lifting legacy detail
// keys when first-class columns are empty.
func FromStoreRecord(rec store.AuditRecord) Event {
	details := cloneDetails(rec.Details)
	ev := Event{
		ID:             rec.ID,
		Timestamp:      rec.Timestamp,
		Actor:          rec.Actor,
		Action:         rec.Action,
		ResourceType:   rec.ResourceType,
		ResourceID:     rec.ResourceID,
		Outcome:        rec.Outcome,
		Tenant:         rec.Tenant,
		Subject:        rec.Subject,
		ViewName:       rec.ViewName,
		ToolName:       rec.ToolName,
		ConversationID: rec.ConversationID,
		ModuleName:     rec.ModuleName,
		BlobKey:        rec.BlobKey,
		Details:        details,
	}
	if ev.Tenant == "" {
		ev.Tenant = popDetail(details, "tenant")
	}
	if ev.Subject == "" {
		ev.Subject = popDetail(details, "subject")
	}
	if ev.ViewName == "" {
		ev.ViewName = popDetail(details, "viewName")
	}
	if ev.ToolName == "" {
		ev.ToolName = popDetail(details, "toolName")
	}
	if ev.ModuleName == "" {
		ev.ModuleName = popDetail(details, "moduleName")
	}
	if ev.BlobKey == "" {
		ev.BlobKey = popDetail(details, "blobKey")
	}
	if len(details) == 0 {
		ev.Details = nil
	} else {
		ev.Details = details
	}
	return ev
}

func cloneDetails(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func popDetail(details map[string]string, key string) string {
	if details == nil {
		return ""
	}
	v, ok := details[key]
	if !ok {
		return ""
	}
	delete(details, key)
	return v
}
