package subscriptions

import (
	"context"
	"fmt"
)

// LocalDispatcher invokes registered in-process handlers.
type LocalDispatcher struct {
	Registry *HandlerRegistry
}

// Dispatch calls the configured local handler.
func (d *LocalDispatcher) Dispatch(ctx context.Context, cfg LocalConfig, payload DeliverPayload, resourceJSON []byte) error {
	if d == nil || d.Registry == nil {
		return ErrNilStore
	}
	handler, ok := d.Registry.Lookup(cfg.HandlerName)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownHandler, cfg.HandlerName)
	}
	return handler(ctx, payload, resourceJSON, cfg.Metadata)
}
