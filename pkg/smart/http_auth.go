package smart

import (
	"context"
	"strings"

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
	if bundle, ok := c.bundleFor(ctx, principal, tenant); ok && c.Adapter != nil {
		if !c.scopeAllows(bundle, resourceType, OpRead) {
			return auth.Deny("scope does not grant read access"), nil
		}
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
	if bundle, ok := c.bundleFor(ctx, principal, tenant); ok && c.Adapter != nil {
		if !c.scopeAllowsWrite(bundle, operation, resourceType) {
			return auth.Deny("scope does not grant write access"), nil
		}
		return c.Engine.CanWriteResource(ctx, c.Adapter.ToWriteRequest(bundle, operation, resourceType, id))
	}
	return c.Engine.CanWriteResource(ctx, auth.WriteRequest{
		Principal: principal, Tenant: tenant, Operation: operation,
		ResourceType: resourceType, ID: id,
	})
}

// AuthorizeSearch implements http.AuthChecker.
func (c ScopePolicyAuthChecker) AuthorizeSearch(ctx context.Context, principal auth.Principal, tenant auth.TenantContext, resourceType string) (auth.Decision, error) {
	if c.Engine == nil {
		return auth.Deny("auth engine not configured"), nil
	}
	if bundle, ok := c.bundleFor(ctx, principal, tenant); ok && c.Adapter != nil {
		if !c.scopeAllows(bundle, resourceType, OpSearch) {
			return auth.Deny("scope does not grant search access"), nil
		}
		return c.Engine.CanReadResource(ctx, c.Adapter.ToReadRequest(bundle, resourceType, ""))
	}
	return c.Engine.CanReadResource(ctx, auth.ReadRequest{
		Principal: principal, Tenant: tenant, ResourceType: resourceType,
	})
}

func (c ScopePolicyAuthChecker) bundleFor(ctx context.Context, principal auth.Principal, tenant auth.TenantContext) (AuthBundle, bool) {
	if c.BundleFor != nil {
		if bundle, ok := c.BundleFor(principal, tenant); ok {
			return bundle, true
		}
	}
	if bundle, ok := AuthBundleFromContext(ctx); ok {
		return bundle, true
	}
	return AuthBundle{}, false
}

func (c ScopePolicyAuthChecker) scopeAllows(bundle AuthBundle, resourceType string, op AccessOp) bool {
	actor := actorForKind(bundle.Principal.Kind, bundle.Scopes)
	if actor == "" {
		return false
	}
	return bundle.Scopes.AllowsOp(actor, resourceType, op)
}

func (c ScopePolicyAuthChecker) scopeAllowsWrite(bundle AuthBundle, operation, resourceType string) bool {
	op := writeOperationToAccessOp(operation)
	return c.scopeAllows(bundle, resourceType, op)
}

func writeOperationToAccessOp(operation string) AccessOp {
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "create":
		return OpCreate
	case "update", "patch":
		return OpUpdate
	case "delete":
		return OpDelete
	default:
		return OpUpdate
	}
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
