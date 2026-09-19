package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/hooks"
)

func (h *handler) runIncoming(ctx context.Context, route parsedRoute, method string) error {
	if h == nil || h.cfg.Hooks == nil {
		return nil
	}
	event := incomingEvent(route, method)
	if err := h.cfg.Hooks.Run(ctx, hooks.Incoming, event); err != nil {
		var svcErr *core.ServiceError
		if errors.As(err, &svcErr) {
			return err
		}
		return invalidRequest("incoming hook rejected request", err)
	}
	return nil
}

func incomingEvent(route parsedRoute, method string) *hooks.Event {
	return &hooks.Event{
		Action:       actionFromRoute(route, method),
		ResourceType: route.resourceType,
		ID:           route.id,
		Operation:    route.operation,
	}
}

func actionFromRoute(route parsedRoute, method string) hooks.Action {
	switch route.kind {
	case routeMetadata:
		return hooks.ActionMetadata
	case routeTransaction:
		return hooks.ActionTransaction
	case routeSystemSearch, routeTypeSearch:
		return hooks.ActionSearch
	case routeHistory:
		return hooks.ActionHistory
	case routeOperation, routeSystemOperation:
		return hooks.ActionOperation
	case routeType:
		switch method {
		case http.MethodGet:
			return hooks.ActionSearch
		case http.MethodPost:
			return hooks.ActionCreate
		case http.MethodPut:
			return hooks.ActionUpdate
		case http.MethodDelete:
			return hooks.ActionDelete
		default:
			return hooks.Action(method)
		}
	case routeInstance:
		switch method {
		case http.MethodGet:
			return hooks.ActionRead
		case http.MethodPut:
			return hooks.ActionUpdate
		case http.MethodPatch:
			return hooks.ActionPatch
		case http.MethodDelete:
			return hooks.ActionDelete
		default:
			return hooks.Action(method)
		}
	default:
		return hooks.Action(method)
	}
}

func withHookContext(w http.ResponseWriter, ctx context.Context, runner hooks.Hooks, route parsedRoute, method string) http.ResponseWriter {
	formatted, ok := w.(*formattedResponseWriter)
	if !ok {
		return w
	}
	formatted.hookCtx = ctx
	formatted.hooks = runner
	formatted.hookEvent = incomingEvent(route, method)
	return formatted
}
