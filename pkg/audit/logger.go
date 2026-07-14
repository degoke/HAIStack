package audit

import (
	"context"
	"errors"
)

var (
	// ErrNilStore is returned when StoreAdapter has no backing AuditStore.
	ErrNilStore = errors.New("audit: audit store is required")
	// ErrNilLogger is returned when an emit helper is called with a nil Logger.
	ErrNilLogger = errors.New("audit: logger is required")
)

// Logger is the shared append seam for canonical audit events.
type Logger interface {
	Log(ctx context.Context, event Event) error
}

// LoggerFunc adapts a function to Logger.
type LoggerFunc func(ctx context.Context, event Event) error

// Log implements Logger.
func (f LoggerFunc) Log(ctx context.Context, event Event) error {
	return f(ctx, event)
}

// NopLogger discards all events.
type NopLogger struct{}

// Log implements Logger.
func (NopLogger) Log(context.Context, Event) error { return nil }
