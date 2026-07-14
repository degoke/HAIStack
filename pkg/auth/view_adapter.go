package auth

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/view"
)

// ActorResolver maps a free-form actor string (as used by view/ai) to a
// Principal and TenantContext for authorization decisions.
type ActorResolver func(ctx context.Context, actor, subject string) (Principal, TenantContext, error)

// ViewAuthorizer adapts Engine to view.Authorizer.
type ViewAuthorizer struct {
	Engine   *Engine
	Resolve  ActorResolver
	TenantID string // fallback tenant when Resolve does not set one
}

var _ view.Authorizer = (*ViewAuthorizer)(nil)

// AuthorizeView implements view.Authorizer. When the view declares permissions,
// the principal must hold at least one of them and pass policy for
// execute-view.
func (a *ViewAuthorizer) AuthorizeView(ctx context.Context, req view.AuthRequest) error {
	if a == nil || a.Engine == nil {
		return fmt.Errorf("%w: engine required", ErrInvalidConfig)
	}
	if a.Resolve == nil {
		return fmt.Errorf("%w", ErrMissingResolver)
	}
	principal, tenant, err := a.Resolve(ctx, req.Actor, req.Subject)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDenied, err)
	}
	if tenant.TenantID == "" {
		tenant.TenantID = a.TenantID
	}
	d, err := a.Engine.CanExecuteView(ctx, ViewRequest{
		Principal:           principal,
		Tenant:              tenant,
		ViewName:            req.ViewName,
		Version:             req.Version,
		ResourceType:        req.ResourceType,
		RequiredPermissions: req.Permissions,
		Parameters:          req.Parameters,
	})
	if err != nil {
		return err
	}
	if !d.Allowed {
		return fmt.Errorf("%w: %s", view.ErrUnauthorized, d.Reason)
	}
	return nil
}
