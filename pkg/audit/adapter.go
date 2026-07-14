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

// List returns store records matching the subset of filters supported by
// store.AuditQuery.
func (a *StoreAdapter) List(ctx context.Context, query store.AuditQuery) ([]store.AuditRecord, error) {
	if a == nil || a.Store == nil {
		return nil, ErrNilStore
	}
	return a.Store.List(ctx, query)
}

// ListEvents lists and converts records, applying Action/Outcome/Tenant filters
// that are not part of store.AuditQuery.
func (a *StoreAdapter) ListEvents(ctx context.Context, query Query) ([]Event, error) {
	if a == nil || a.Store == nil {
		return nil, ErrNilStore
	}
	limit := query.Limit
	fetchLimit := limit
	if query.Action != "" || query.Outcome != "" || query.Tenant != "" {
		// Over-fetch when post-filtering; callers can pass a higher Limit.
		if fetchLimit > 0 {
			fetchLimit = fetchLimit * 4
			if fetchLimit < 64 {
				fetchLimit = 64
			}
		}
	}
	records, err := a.Store.List(ctx, store.AuditQuery{
		ResourceType: query.ResourceType,
		ResourceID:   query.ResourceID,
		Actor:        query.Actor,
		After:        query.After,
		Before:       query.Before,
		Limit:        fetchLimit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(records))
	for _, rec := range records {
		ev := FromStoreRecord(rec)
		if query.Action != "" && ev.Action != query.Action {
			continue
		}
		if query.Outcome != "" && ev.Outcome != query.Outcome {
			continue
		}
		if query.Tenant != "" && ev.Tenant != query.Tenant {
			continue
		}
		out = append(out, ev)
		if limit > 0 && len(out) >= limit {
			break
		}
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

// ToStoreRecord converts an Event into store.AuditRecord, folding extended
// fields into Details.
func ToStoreRecord(event Event) store.AuditRecord {
	details := cloneDetails(event.Details)
	if event.Tenant != "" {
		details["tenant"] = event.Tenant
	}
	if event.Subject != "" {
		details["subject"] = event.Subject
	}
	if event.ViewName != "" {
		details["viewName"] = event.ViewName
	}
	if event.ToolName != "" {
		details["toolName"] = event.ToolName
	}
	if event.ModuleName != "" {
		details["moduleName"] = event.ModuleName
	}
	if event.BlobKey != "" {
		details["blobKey"] = event.BlobKey
	}
	return store.AuditRecord{
		ID:           event.ID,
		Timestamp:    event.Timestamp,
		Actor:        event.Actor,
		Action:       event.Action,
		ResourceType: event.ResourceType,
		ResourceID:   event.ResourceID,
		Outcome:      event.Outcome,
		Details:      details,
	}
}

// FromStoreRecord converts a store.AuditRecord into Event, lifting known detail
// keys into first-class fields.
func FromStoreRecord(rec store.AuditRecord) Event {
	details := cloneDetails(rec.Details)
	ev := Event{
		ID:           rec.ID,
		Timestamp:    rec.Timestamp,
		Actor:        rec.Actor,
		Action:       rec.Action,
		ResourceType: rec.ResourceType,
		ResourceID:   rec.ResourceID,
		Outcome:      rec.Outcome,
		Details:      details,
	}
	ev.Tenant = popDetail(details, "tenant")
	ev.Subject = popDetail(details, "subject")
	ev.ViewName = popDetail(details, "viewName")
	ev.ToolName = popDetail(details, "toolName")
	ev.ModuleName = popDetail(details, "moduleName")
	ev.BlobKey = popDetail(details, "blobKey")
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
