package smart

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// ScopeFilterMatcher evaluates whether a resource satisfies one SMART scope filter parameter.
// Implementations return known=false when the parameter is outside their domain so callers
// can fall back to the built-in MVP matcher.
type ScopeFilterMatcher interface {
	Match(ctx context.Context, resourceType string, resource *types.ResourceEnvelope, param string, want []string) (matched bool, known bool)
}

// ScopeFilterMatcherFunc adapts a function to ScopeFilterMatcher.
type ScopeFilterMatcherFunc func(ctx context.Context, resourceType string, resource *types.ResourceEnvelope, param string, want []string) (matched bool, known bool)

// Match implements ScopeFilterMatcher.
func (f ScopeFilterMatcherFunc) Match(ctx context.Context, resourceType string, resource *types.ResourceEnvelope, param string, want []string) (matched bool, known bool) {
	if f == nil {
		return false, false
	}
	return f(ctx, resourceType, resource, param, want)
}

// ChainedScopeFilterMatcher tries matchers in order and uses the first known result.
type ChainedScopeFilterMatcher []ScopeFilterMatcher

// Match implements ScopeFilterMatcher.
func (c ChainedScopeFilterMatcher) Match(ctx context.Context, resourceType string, resource *types.ResourceEnvelope, param string, want []string) (matched bool, known bool) {
	for _, matcher := range c {
		if matcher == nil {
			continue
		}
		matched, known := matcher.Match(ctx, resourceType, resource, param, want)
		if known {
			return matched, true
		}
	}
	return false, false
}

var scopeFilterMatcher ScopeFilterMatcher = mvpScopeFilterMatcher{}

// SetScopeFilterMatcher replaces the global scope filter matcher used by enforcement helpers.
// Hosts typically chain a registry-backed matcher ahead of the built-in MVP matcher.
func SetScopeFilterMatcher(matcher ScopeFilterMatcher) {
	if matcher == nil {
		scopeFilterMatcher = mvpScopeFilterMatcher{}
		return
	}
	scopeFilterMatcher = matcher
}

// DefaultScopeFilterMatcher returns the currently configured matcher.
func DefaultScopeFilterMatcher() ScopeFilterMatcher {
	return scopeFilterMatcher
}

// MVPScopeFilterMatcher returns the built-in fallback matcher.
func MVPScopeFilterMatcher() ScopeFilterMatcher {
	return mvpScopeFilterMatcher{}
}

type mvpScopeFilterMatcher struct{}

func (m mvpScopeFilterMatcher) Match(_ context.Context, resourceType string, resource *types.ResourceEnvelope, param string, want []string) (matched bool, known bool) {
	return mvpResourceMatchesSearchParam(resourceType, resource, param, want), true
}
