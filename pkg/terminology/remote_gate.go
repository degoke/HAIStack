package terminology

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// OptInRemoteGate wraps a remote terminology provider and blocks lookups for
// global CodeSystems the tenant has not opted into via TerminologyInstallStore.
type OptInRemoteGate struct {
	Inner    Provider
	Global   store.TerminologyStore
	Installs store.TerminologyInstallStore
}

// NewOptInRemoteGate constructs a remote provider that respects tenant opt-in.
func NewOptInRemoteGate(inner Provider, global store.TerminologyStore, installs store.TerminologyInstallStore) *OptInRemoteGate {
	return &OptInRemoteGate{Inner: inner, Global: global, Installs: installs}
}

func (g *OptInRemoteGate) Lookup(ctx context.Context, r LookupRequest) (*LookupResult, error) {
	if g.blocked(ctx, r.System, r.Version) {
		return &LookupResult{Found: false}, nil
	}
	return g.Inner.Lookup(ctx, r)
}

func (g *OptInRemoteGate) Expand(ctx context.Context, r ExpandRequest) (*Expansion, error) {
	return g.Inner.Expand(ctx, r)
}

func (g *OptInRemoteGate) ValidateCode(ctx context.Context, r ValidateCodeRequest) (*ValidationResult, error) {
	system := r.Coding.System
	if system == "" && r.URL != "" {
		system = r.URL
	}
	if g.blocked(ctx, system, r.Coding.Version) {
		return &ValidationResult{Status: UnknownTerminology, Message: "terminology is not known"}, nil
	}
	return g.Inner.ValidateCode(ctx, r)
}

func (g *OptInRemoteGate) blocked(ctx context.Context, url, ver string) bool {
	if g.Inner == nil || g.Global == nil || g.Installs == nil || url == "" {
		return false
	}
	if !globalCodeSystemExists(ctx, g.Global, url, ver) {
		return false
	}
	layered := &LayeredStore{Store: g.Global, Installs: g.Installs, GlobalScopeID: GlobalScopeID}
	return !layered.globalAllowed(ctx, url, ver)
}

func globalCodeSystemExists(ctx context.Context, st store.TerminologyStore, url, ver string) bool {
	if ver != "" {
		rec, err := st.FindResource(ctx, GlobalScopeID, "CodeSystem", url, ver)
		return err == nil && rec != nil
	}
	resources, err := st.ListResources(ctx, GlobalScopeID, "CodeSystem")
	if err != nil {
		return false
	}
	for _, rec := range resources {
		if rec.CanonicalURL == url {
			return true
		}
	}
	return false
}
