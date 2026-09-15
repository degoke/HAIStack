package smart

import "context"

type scopeFilterMatcherKey struct{}

// ContextWithScopeFilterMatcher attaches a per-request scope filter matcher.
// Handlers should prefer this over the package-global matcher from SetScopeFilterMatcher.
func ContextWithScopeFilterMatcher(ctx context.Context, matcher ScopeFilterMatcher) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if matcher == nil {
		return ctx
	}
	return context.WithValue(ctx, scopeFilterMatcherKey{}, matcher)
}

// ScopeFilterMatcherFromContext returns the request matcher when present, otherwise
// the configured default matcher.
func ScopeFilterMatcherFromContext(ctx context.Context) ScopeFilterMatcher {
	if ctx != nil {
		if matcher, ok := ctx.Value(scopeFilterMatcherKey{}).(ScopeFilterMatcher); ok && matcher != nil {
			return matcher
		}
	}
	return DefaultScopeFilterMatcher()
}
