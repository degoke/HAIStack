package terminology

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// PreExpandSkipReason explains why a ValueSet was not pre-expanded.
type PreExpandSkipReason string

const (
	SkipNotFound            PreExpandSkipReason = "not-found"
	SkipServerExpansion     PreExpandSkipReason = "server-expansion"
	SkipAlreadyExpanded     PreExpandSkipReason = "already-expanded"
	SkipTooCostly           PreExpandSkipReason = "too-costly"
	SkipMissingCodeSystem   PreExpandSkipReason = "missing-code-system"
	SkipOptInRequired       PreExpandSkipReason = "opt-in-required"
	SkipNoCompose           PreExpandSkipReason = "no-compose"
)

// PreExpandResult reports the outcome of a single ValueSet pre-expand attempt.
type PreExpandResult struct {
	URL     string
	Version string
	Skipped bool
	Reason  PreExpandSkipReason
	Members int
}

// PreExpandOptions configures optional ValueSet pre-expansion.
type PreExpandOptions struct {
	MaxExpansion int
	Installs     store.TerminologyInstallStore
}

// StoreForScope returns the terminology store view used to compose expansions in scope.
func StoreForScope(tenant store.TerminologyStore, tenantScope string, installs store.TerminologyInstallStore, scopeID string) store.TerminologyStore {
	if scopeID == GlobalScopeID {
		return tenant
	}
	layered := NewLayeredStore(tenant, tenantScope)
	layered.Installs = installs
	layered.GlobalScopeID = GlobalScopeID
	return layered
}

// PreExpandScope expands ValueSets in a scope, optionally filtering to specific URLs.
func PreExpandScope(ctx context.Context, st store.TerminologyStore, scopeID string, urls []string, opts PreExpandOptions) ([]PreExpandResult, error) {
	if st == nil {
		return nil, fmt.Errorf("terminology store is required")
	}
	if scopeID == "" {
		return nil, fmt.Errorf("scope is required")
	}
	resources, err := st.ListResources(ctx, scopeID, "ValueSet")
	if err != nil {
		return nil, err
	}
	filter := map[string]bool{}
	for _, u := range urls {
		if u != "" {
			filter[u] = true
		}
	}
	var results []PreExpandResult
	for _, r := range resources {
		if len(filter) > 0 && !filter[r.CanonicalURL] {
			continue
		}
		res, err := PreExpandValueSet(ctx, st, scopeID, r.CanonicalURL, r.Version, r.ResourceJSON, opts)
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}

// PreExpandValueSet composes and persists expansion members for one ValueSet.
func PreExpandValueSet(ctx context.Context, st store.TerminologyStore, scopeID, url, version string, canonicalJSON []byte, opts PreExpandOptions) (PreExpandResult, error) {
	result := PreExpandResult{URL: url, Version: version}
	if url == "" {
		result.Skipped = true
		result.Reason = SkipNotFound
		return result, nil
	}
	if len(canonicalJSON) == 0 {
		rec, err := st.FindResource(ctx, scopeID, "ValueSet", url, version)
		if err != nil {
			return result, err
		}
		if rec == nil {
			result.Skipped = true
			result.Reason = SkipNotFound
			return result, nil
		}
		canonicalJSON = rec.ResourceJSON
	}
	var raw map[string]any
	if err := json.Unmarshal(canonicalJSON, &raw); err != nil {
		return result, err
	}
	if hasServerExpansion(raw) {
		result.Skipped = true
		result.Reason = SkipServerExpansion
		return result, nil
	}
	vs, err := st.GetValueSet(ctx, scopeID, url, version)
	if err != nil {
		return result, err
	}
	if vs == nil || vs.ComposeJSON == "" {
		result.Skipped = true
		result.Reason = SkipNoCompose
		return result, nil
	}
	members, err := st.ListValueSetMembers(ctx, scopeID, url, version)
	if err != nil {
		return result, err
	}
	fp := ComposeFingerprint(vs.ComposeJSON)
	if len(members) > 0 && vs.ExpansionFingerprint == fp {
		result.Skipped = true
		result.Reason = SkipAlreadyExpanded
		result.Members = len(members)
		return result, nil
	}
	if reason := composeDependenciesBlocked(ctx, st, opts.Installs, scopeID, vs.ComposeJSON); reason != "" {
		result.Skipped = true
		result.Reason = reason
		return result, nil
	}
	composed, err := composeMembers(ctx, st, scopeID, vs.ComposeJSON, map[string]bool{url + "|" + version: true})
	if err != nil {
		return result, err
	}
	max := opts.MaxExpansion
	if max <= 0 {
		max = 10000
	}
	if len(composed) > max {
		result.Skipped = true
		result.Reason = SkipTooCostly
		return result, nil
	}
	for i := range composed {
		composed[i].ScopeID = scopeID
		composed[i].ValueSetURL = url
		composed[i].ValueSetVersion = version
	}
	record := *vs
	record.ExpansionFingerprint = fp
	record.ExpansionTimestamp = time.Now().UTC().Format(time.RFC3339)
	if err := st.ReplaceValueSet(ctx, record, composed); err != nil {
		return result, err
	}
	result.Members = len(composed)
	return result, nil
}

func hasServerExpansion(r map[string]any) bool {
	exp, ok := r["expansion"].(map[string]any)
	if !ok {
		return false
	}
	contains, ok := exp["contains"].([]any)
	return ok && len(contains) > 0
}

func composeDependenciesBlocked(ctx context.Context, st store.TerminologyStore, installs store.TerminologyInstallStore, scopeID, composeJSON string) PreExpandSkipReason {
	var c map[string]any
	if err := json.Unmarshal([]byte(composeJSON), &c); err != nil {
		return ""
	}
	inc, _ := c["include"].([]any)
	for _, iv := range inc {
		m, _ := iv.(map[string]any)
		sys, _ := m["system"].(string)
		if sys == "" {
			continue
		}
		if scopeID == GlobalScopeID {
			if !codeSystemExistsInScope(ctx, st, GlobalScopeID, sys) {
				return SkipMissingCodeSystem
			}
			continue
		}
		local, err := st.FindResource(ctx, scopeID, "CodeSystem", sys, "")
		if err != nil {
			return ""
		}
		if local != nil {
			continue
		}
		global, err := st.FindResource(ctx, GlobalScopeID, "CodeSystem", sys, "")
		if err != nil {
			return ""
		}
		if global == nil {
			return SkipMissingCodeSystem
		}
		if installs == nil {
			return SkipOptInRequired
		}
		layered := &LayeredStore{Store: st, TenantScopeID: scopeID, GlobalScopeID: GlobalScopeID, Installs: installs}
		if !layered.globalAllowed(ctx, sys, "", "CodeSystem") {
			return SkipOptInRequired
		}
	}
	return ""
}

func codeSystemExistsInScope(ctx context.Context, st store.TerminologyStore, scopeID, url string) bool {
	resources, err := st.ListResources(ctx, scopeID, "CodeSystem")
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
