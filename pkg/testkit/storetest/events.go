package storetest

import (
	"context"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
)

var _ store.EventStore = (*EventStore)(nil)

// EventStore is an in-memory EventStore with monotonic sequence assignment.
type EventStore struct {
	mu       sync.Mutex
	events   []store.ResourceEvent
	sequence int64
}

// NewEventStore returns an empty in-memory event store.
func NewEventStore() *EventStore {
	return &EventStore{}
}

func (s *EventStore) Append(_ context.Context, event store.ResourceEvent) (store.ResourceEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence++
	event.Sequence = s.sequence
	s.events = append(s.events, event)
	return event, nil
}

func (s *EventStore) ReadSince(_ context.Context, afterSequence int64, limit int) ([]store.ResourceEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.ResourceEvent
	for _, event := range s.events {
		if event.Sequence <= afterSequence {
			continue
		}
		out = append(out, event)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *EventStore) LatestForResource(_ context.Context, resourceType, id string) (*store.ResourceEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.events) - 1; i >= 0; i-- {
		event := s.events[i]
		if event.ResourceType == resourceType && event.ID == id {
			copy := event
			return &copy, nil
		}
	}
	return nil, nil
}

// Events returns a copy of all stored events.
func (s *EventStore) Events() []store.ResourceEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.ResourceEvent, len(s.events))
	copy(out, s.events)
	return out
}

func (s *EventStore) clone() *EventStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &EventStore{
		events:   append([]store.ResourceEvent(nil), s.events...),
		sequence: s.sequence,
	}
}

func (s *EventStore) replaceFrom(source *EventStore) {
	source.mu.Lock()
	events := append([]store.ResourceEvent(nil), source.events...)
	sequence := source.sequence
	source.mu.Unlock()
	s.mu.Lock()
	s.events = events
	s.sequence = sequence
	s.mu.Unlock()
}
