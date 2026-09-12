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
	SkipNotFound          PreExpandSkipReason = "not-found"
	SkipServerExpansion   PreExpandSkipReason = "server-expansion"
	SkipAlreadyExpanded   PreExpandSkipReason = "already-expanded"
	SkipTooCostly         PreExpandSkipReason = "too-costly"
	SkipMissingCodeSystem PreExpandSkipReason = "missing-code-system"
	SkipMissingValueSet   PreExpandSkipReason = "missing-valueset"
	SkipOptInRequired     PreExpandSkipReason = "opt-in-required"
	SkipNoCompose         PreExpandSkipReason = "no-compose"
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

// ShouldEnqueuePreExpand reports whether a ValueSet is worth enqueueing for pre-expand.
// Full CodeSystem inclusions without explicit concepts are treated as potentially unbounded.
func ShouldEnqueuePreExpand(raw []byte) bool {
	var r map[string]any
	if err := json.Unmarshal(raw, &r); err != nil {
		return false
	}
	if hasServerExpansion(r) {
		return false
	}
	compose, ok := r["compose"].(map[string]any)
	if !ok {
		return false
	}
	includes, ok := compose["include"].([]any)
	if !ok || len(includes) == 0 {
		return false
	}
	for _, iv := range includes {
		m, _ := iv.(map[string]any)
		sys, _ := m["system"].(string)
		concepts, hasConcept := m["concept"].([]any)
		vals, hasValueSet := m["valueSet"].([]any)
		if sys != "" && (!hasConcept || len(concepts) == 0) {
			return false
		}
		if hasValueSet && len(vals) > 0 && sys == "" && (!hasConcept || len(concepts) == 0) {
			return false
		}
	}
	return true
}

// EligiblePackPreExpandURLs lists ValueSet canonical URLs in a package version
// that pass ShouldEnqueuePreExpand heuristics.
func EligiblePackPreExpandURLs(ctx context.Context, definitions store.DefinitionStore, listStore store.TerminologyStore, scopeID, packName, packVersion string) ([]string, error) {
	if definitions == nil {
		return nil, fmt.Errorf("definition store is required")
	}
	if packName == "" {
		return nil, fmt.Errorf("packName is required")
	}
	if packVersion == "" {
		return nil, fmt.Errorf("packVersion is required")
	}
	defs, err := definitions.List(ctx, store.DefinitionFilter{PackageName: packName})
	if err != nil {
		return nil, err
	}
	var urls []string
	for _, def := range defs {
		if def.PackageVersion != packVersion {
			continue
		}
		if def.FHIRResourceType != "ValueSet" {
			continue
		}
		var raw []byte
		if listStore != nil {
			rec, err := listStore.FindResource(ctx, scopeID, "ValueSet", def.CanonicalURL, def.Version)
			if err == nil && rec != nil {
				raw = rec.ResourceJSON
			}
		}
		if len(raw) == 0 {
			raw = def.JSONData
		}
		if !ShouldEnqueuePreExpand(raw) {
			continue
		}
		urls = append(urls, def.CanonicalURL)
	}
	return urls, nil
}

// PreExpandPack pre-expands finite ValueSets from one installed package.
func PreExpandPack(ctx context.Context, listStore, composeStore store.TerminologyStore, definitions store.DefinitionStore, scopeID, packName, packVersion string, opts PreExpandOptions) ([]PreExpandResult, error) {
	urls, err := EligiblePackPreExpandURLs(ctx, definitions, listStore, scopeID, packName, packVersion)
	if err != nil {
		return nil, err
	}
	if len(urls) == 0 {
		return nil, nil
	}
	return PreExpandScope(ctx, listStore, composeStore, scopeID, urls, opts)
}

// PreExpandScope expands ValueSets stored in scopeID using listStore for enumeration
// and composeStore for dependency resolution during composition.
func PreExpandScope(ctx context.Context, listStore, composeStore store.TerminologyStore, scopeID string, urls []string, opts PreExpandOptions) ([]PreExpandResult, error) {
	if listStore == nil {
		return nil, fmt.Errorf("terminology store is required")
	}
	if scopeID == "" {
		return nil, fmt.Errorf("scope is required")
	}
	if composeStore == nil {
		composeStore = listStore
	}
	resources, err := listStore.ListResources(ctx, scopeID, "ValueSet")
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
		if r.ScopeID != "" && r.ScopeID != scopeID {
			continue
		}
		if len(filter) > 0 && !filter[r.CanonicalURL] {
			continue
		}
		res, err := PreExpandValueSet(ctx, listStore, composeStore, scopeID, r.CanonicalURL, r.Version, r.ResourceJSON, opts)
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}

// PreExpandValueSet composes and persists expansion members for one ValueSet.
func PreExpandValueSet(ctx context.Context, store, composeStore store.TerminologyStore, scopeID, url, version string, canonicalJSON []byte, opts PreExpandOptions) (PreExpandResult, error) {
	result := PreExpandResult{URL: url, Version: version}
	if store == nil {
		return result, fmt.Errorf("terminology store is required")
	}
	if composeStore == nil {
		composeStore = store
	}
	if url == "" {
		result.Skipped = true
		result.Reason = SkipNotFound
		return result, nil
	}
	if len(canonicalJSON) == 0 {
		rec, err := store.FindResource(ctx, scopeID, "ValueSet", url, version)
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
		members, err := store.ListValueSetMembers(ctx, scopeID, url, version)
		if err != nil {
			return result, err
		}
		if len(members) > 0 {
			result.Skipped = true
			result.Reason = SkipServerExpansion
			result.Members = len(members)
			return result, nil
		}
		persisted, err := persistServerExpansion(ctx, store, scopeID, url, version, raw)
		if err != nil {
			return result, err
		}
		result.Members = persisted
		return result, nil
	}
	vs, err := store.GetValueSet(ctx, scopeID, url, version)
	if err != nil {
		return result, err
	}
	if vs == nil || vs.ComposeJSON == "" {
		result.Skipped = true
		result.Reason = SkipNoCompose
		return result, nil
	}
	members, err := store.ListValueSetMembers(ctx, scopeID, url, version)
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
	if reason := composeDependenciesBlocked(ctx, composeStore, opts.Installs, scopeID, vs.ComposeJSON); reason != "" {
		result.Skipped = true
		result.Reason = reason
		return result, nil
	}
	composed, err := composeMembers(ctx, composeStore, scopeID, vs.ComposeJSON, map[string]bool{url + "|" + version: true})
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
	if err := store.ReplaceValueSet(ctx, record, composed); err != nil {
		return result, err
	}
	result.Members = len(composed)
	return result, nil
}

func persistServerExpansion(ctx context.Context, st store.TerminologyStore, scopeID, url, version string, raw map[string]any) (int, error) {
	vs, err := st.GetValueSet(ctx, scopeID, url, version)
	if err != nil {
		return 0, err
	}
	if vs == nil {
		return 0, nil
	}
	members := membersFromFHIRExpansion(scopeID, url, version, raw)
	if len(members) == 0 {
		return 0, nil
	}
	record := *vs
	record.ExpansionTimestamp = time.Now().UTC().Format(time.RFC3339)
	if err := st.ReplaceValueSet(ctx, record, members); err != nil {
		return 0, err
	}
	return len(members), nil
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
		if reason := includeDependencyBlocked(ctx, st, installs, scopeID, m); reason != "" {
			return reason
		}
	}
	return ""
}

func includeDependencyBlocked(ctx context.Context, st store.TerminologyStore, installs store.TerminologyInstallStore, scopeID string, include map[string]any) PreExpandSkipReason {
	sys, _ := include["system"].(string)
	if sys != "" {
		if reason := codeSystemDependencyBlocked(ctx, st, installs, scopeID, sys); reason != "" {
			return reason
		}
	}
	vals, _ := include["valueSet"].([]any)
	for _, vv := range vals {
		u, _ := vv.(string)
		if u == "" {
			continue
		}
		if reason := valueSetDependencyBlocked(ctx, st, installs, scopeID, u); reason != "" {
			return reason
		}
	}
	return ""
}

func codeSystemDependencyBlocked(ctx context.Context, st store.TerminologyStore, installs store.TerminologyInstallStore, scopeID, url string) PreExpandSkipReason {
	if scopeID == GlobalScopeID {
		if !codeSystemExistsInScope(ctx, st, GlobalScopeID, url) {
			return SkipMissingCodeSystem
		}
		return ""
	}
	if codeSystemExistsInScope(ctx, st, scopeID, url) {
		return ""
	}
	if !codeSystemExistsInScope(ctx, st, GlobalScopeID, url) {
		return SkipMissingCodeSystem
	}
	if installs == nil {
		return SkipOptInRequired
	}
	layered := &LayeredStore{Store: st, TenantScopeID: scopeID, GlobalScopeID: GlobalScopeID, Installs: installs}
	if !layered.globalAllowed(ctx, url, "", "CodeSystem") {
		return SkipOptInRequired
	}
	return ""
}

func valueSetDependencyBlocked(ctx context.Context, st store.TerminologyStore, installs store.TerminologyInstallStore, scopeID, url string) PreExpandSkipReason {
	if scopeID == GlobalScopeID {
		if !valueSetExistsInScope(ctx, st, GlobalScopeID, url) {
			return SkipMissingValueSet
		}
		return ""
	}
	if valueSetExistsInScope(ctx, st, scopeID, url) {
		return ""
	}
	if !valueSetExistsInScope(ctx, st, GlobalScopeID, url) {
		return SkipMissingValueSet
	}
	if installs == nil {
		return SkipOptInRequired
	}
	layered := &LayeredStore{Store: st, TenantScopeID: scopeID, GlobalScopeID: GlobalScopeID, Installs: installs}
	if !layered.globalAllowed(ctx, url, "", "ValueSet") {
		return SkipOptInRequired
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

func valueSetExistsInScope(ctx context.Context, st store.TerminologyStore, scopeID, url string) bool {
	resources, err := st.ListResources(ctx, scopeID, "ValueSet")
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
