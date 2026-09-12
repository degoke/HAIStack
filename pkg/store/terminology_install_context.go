package store

import "context"

type terminologyInstallsContextKey struct{}
type terminologyScopeContextKey struct{}

// ContextWithTerminologyInstalls overrides the terminology opt-in store for one call tree.
func ContextWithTerminologyInstalls(ctx context.Context, installs TerminologyInstallStore) context.Context {
	if installs == nil {
		return ctx
	}
	return context.WithValue(ctx, terminologyInstallsContextKey{}, installs)
}

// TerminologyInstallsFromContext returns a per-request terminology opt-in store when set.
func TerminologyInstallsFromContext(ctx context.Context) TerminologyInstallStore {
	if ctx == nil {
		return nil
	}
	installs, _ := ctx.Value(terminologyInstallsContextKey{}).(TerminologyInstallStore)
	return installs
}

// ContextWithTerminologyScope overrides the tenant terminology overlay scope for one call tree.
func ContextWithTerminologyScope(ctx context.Context, scopeID string) context.Context {
	if scopeID == "" {
		return ctx
	}
	return context.WithValue(ctx, terminologyScopeContextKey{}, scopeID)
}

// TerminologyScopeFromContext returns a per-request terminology overlay scope when set.
func TerminologyScopeFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	scope, _ := ctx.Value(terminologyScopeContextKey{}).(string)
	return scope
}
