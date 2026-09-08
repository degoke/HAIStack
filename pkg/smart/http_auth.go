package smart

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/auth"
)

// ScopePolicyAuthChecker adapts auth.PolicyEngine to pkg/http.AuthChecker while
// passing SMART scope-derived RequiredPermissions into read/write/search decisions.
// Pair with BundleFor to supply the validated SMART AuthBundle per request.
type ScopePolicyAuthChecker struct {
	Engine  auth.PolicyEngine
	Adapter *AuthAdapter
	// BundleFor returns the SMART auth bundle for the authenticated principal.
	// When it returns false, requests are evaluated without scope-derived permissions.
	BundleFor func(principal auth.Principal, tenant auth.TenantContext) (AuthBundle, bool)
}

// AuthorizeRead implements http.AuthChecker.
func (c ScopePolicyAuthChecker) AuthorizeRead(ctx context.Context, principal auth.Principal, tenant auth.TenantContext, resourceType, id string) (auth.Decision, error) {
	if c.Engine == nil {
		return auth.Deny("auth engine not configured"), nil
	}
	if bundle, ok := c.bundleFor(principal, tenant); ok && c.Adapter != nil {
		return c.Engine.CanReadResource(ctx, c.Adapter.ToReadRequest(bundle, resourceType, id))
	}
	return c.Engine.CanReadResource(ctx, auth.ReadRequest{
		Principal: principal, Tenant: tenant, ResourceType: resourceType, ID: id,
	})
}

// AuthorizeWrite implements http.AuthChecker.
func (c ScopePolicyAuthChecker) AuthorizeWrite(ctx context.Context, principal auth.Principal, tenant auth.TenantContext, operation, resourceType, id string) (auth.Decision, error) {
	if c.Engine == nil {
		return auth.Deny("auth engine not configured"), nil
	}
	if bundle, ok := c.bundleFor(principal, tenant); ok && c.Adapter != nil {
		return c.Engine.CanWriteResource(ctx, c.Adapter.ToWriteRequest(bundle, operation, resourceType, id))
	}
	return c.Engine.CanWriteResource(ctx, auth.WriteRequest{
		Principal: principal, Tenant: tenant, Operation: operation,
		ResourceType: resourceType, ID: id,
	})
}

// AuthorizeSearch implements http.AuthChecker.
func (c ScopePolicyAuthChecker) AuthorizeSearch(ctx context.Context, principal auth.Principal, tenant auth.TenantContext, resourceType string) (auth.Decision, error) {
	return c.AuthorizeRead(ctx, principal, tenant, resourceType, "")
}

func (c ScopePolicyAuthChecker) bundleFor(principal auth.Principal, tenant auth.TenantContext) (AuthBundle, bool) {
	if c.BundleFor == nil {
		return AuthBundle{}, false
	}
	return c.BundleFor(principal, tenant)
}

type authBundleContextKey struct{}

// ContextWithAuthBundle stores a validated SMART AuthBundle on the context.
func ContextWithAuthBundle(ctx context.Context, bundle AuthBundle) context.Context {
	return context.WithValue(ctx, authBundleContextKey{}, bundle)
}

// AuthBundleFromContext retrieves a SMART AuthBundle previously stored on ctx.
func AuthBundleFromContext(ctx context.Context) (AuthBundle, bool) {
	v, ok := ctx.Value(authBundleContextKey{}).(AuthBundle)
	return v, ok
}
