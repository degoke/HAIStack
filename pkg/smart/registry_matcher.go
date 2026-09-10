package smart

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// RegistryScopeFilterMatcher evaluates SMART scope filters using registry SearchParameters.
type RegistryScopeFilterMatcher struct {
	registry search.Registry
	engine   fhirpath.Engine
}

// NewRegistryScopeFilterMatcher constructs a matcher backed by compiled search metadata.
func NewRegistryScopeFilterMatcher(registry search.Registry, engine fhirpath.Engine) *RegistryScopeFilterMatcher {
	return &RegistryScopeFilterMatcher{registry: registry, engine: engine}
}

// Match implements ScopeFilterMatcher.
func (m *RegistryScopeFilterMatcher) Match(ctx context.Context, resourceType string, resource *types.ResourceEnvelope, param string, want []string) (matched bool, known bool) {
	if m == nil {
		return false, false
	}
	return search.MatchResourceParameter(ctx, m.registry, m.engine, resourceType, resource, param, want)
}

// InstallRegistryScopeFilterMatcher registers a registry-backed matcher ahead of the MVP matcher.
func InstallRegistryScopeFilterMatcher(registry search.Registry, engine fhirpath.Engine) {
	SetScopeFilterMatcher(ChainedScopeFilterMatcher{
		NewRegistryScopeFilterMatcher(registry, engine),
		MVPScopeFilterMatcher(),
	})
}
