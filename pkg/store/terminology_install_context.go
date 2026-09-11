package store

import "context"

type terminologyInstallsContextKey struct{}

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
