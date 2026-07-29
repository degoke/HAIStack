// Package terminology provides local FHIR R4 CodeSystem and ValueSet services.
package terminology

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
)

type Coding struct{ System, Version, Code, Display string }
type CodeableConcept struct {
	Coding []Coding
	Text   string
}

// Concept is the public representation of a compiled code.
type Concept struct {
	System, Version, Code, Display, Definition string
	Active, Abstract                           bool
}
type LookupRequest struct{ ScopeID, System, Version, Code string }
type LookupResult struct {
	Concept Concept
	Found   bool
}

type ExpandRequest struct {
	ScopeID, URL, Version string
	Offset, Count         int
}
type Expansion struct {
	URL, Version string
	Total        int
	Contains     []Coding
}
type ValidateCodeRequest struct {
	ScopeID, URL, Version string
	Coding                Coding
	Concept               *CodeableConcept
}

// ValidateCodeableConcept validates the first coding that is known to the
// requested terminology. It reports valid when any coding is valid.
func (s *LocalService) ValidateCodeableConcept(ctx context.Context, scope, url, version string, cc CodeableConcept) (*ValidationResult, error) {
	if len(cc.Coding) == 0 {
		return &ValidationResult{Status: Invalid, Message: "CodeableConcept has no coding"}, nil
	}
	var unknown *ValidationResult
	for _, c := range cc.Coding {
		r, err := s.ValidateCode(ctx, ValidateCodeRequest{ScopeID: scope, URL: url, Version: version, Coding: c})
		if err != nil {
			return nil, err
		}
		if r.Status == Valid {
			return r, nil
		}
		if r.Status == UnknownTerminology {
			unknown = r
		}
	}
	if unknown != nil {
		return unknown, nil
	}
	return &ValidationResult{Status: Invalid, Message: "no coding is valid"}, nil
}

type ResultStatus string

const (
	Valid                ResultStatus = "valid"
	Invalid              ResultStatus = "invalid"
	UnknownTerminology   ResultStatus = "unknown-terminology"
	UnavailableProvider  ResultStatus = "unavailable-provider"
	UnsupportedOperation ResultStatus = "unsupported-operation"
	TooCostly            ResultStatus = "too-costly"
)

type ValidationResult struct {
	Status         ResultStatus
	Message        string
	DisplayWarning bool
}

type Provider interface {
	Lookup(context.Context, LookupRequest) (*LookupResult, error)
	Expand(context.Context, ExpandRequest) (*Expansion, error)
	ValidateCode(context.Context, ValidateCodeRequest) (*ValidationResult, error)
}
type Service interface{ Provider }
type Invalidator interface {
	InvalidateCodeSystem(system, version string)
	InvalidateValueSet(url, version string)
}

// LocalService uses compiled projections and deterministic canonical resolution.
type LocalService struct {
	Store        store.TerminologyStore
	ScopeID      string
	MaxExpansion int
	mu           sync.RWMutex
	lookupCache  map[string]*LookupResult
	expandCache  map[string]*Expansion
}

func (s *LocalService) cachedLookup(key string) (*LookupResult, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.lookupCache[key]
	if !ok {
		return nil, false
	}
	x := *v
	return &x, true
}
func (s *LocalService) InvalidateCodeSystem(system, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.lookupCache {
		if strings.HasPrefix(k, system+"|") && (version == "" || strings.Contains(k, "|"+version+"|")) {
			delete(s.lookupCache, k)
		}
	}
	s.expandCache = map[string]*Expansion{}
}
func (s *LocalService) InvalidateValueSet(url, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.expandCache != nil {
		delete(s.expandCache, url+"|"+version)
	}
}

func (s *LocalService) Lookup(ctx context.Context, r LookupRequest) (*LookupResult, error) {
	if r.System == "" || r.Code == "" {
		return nil, fmt.Errorf("system and code are required")
	}
	cacheKey := r.System + "|" + r.Version + "|" + r.Code
	if v, ok := s.cachedLookup(cacheKey); ok {
		return v, nil
	}
	c, err := s.Store.LookupConcept(ctx, s.ScopeID, r.System, r.Version, r.Code)
	if err != nil {
		return nil, err
	}
	if c == nil {
		v := &LookupResult{Found: false}
		s.mu.Lock()
		if s.lookupCache == nil {
			s.lookupCache = map[string]*LookupResult{}
		}
		s.lookupCache[cacheKey] = v
		s.mu.Unlock()
		return v, nil
	}
	v := &LookupResult{Found: true, Concept: Concept{System: c.SystemURL, Version: c.SystemVersion, Code: c.Code, Display: c.Display, Definition: c.Definition, Active: c.Active, Abstract: c.Abstract}}
	s.mu.Lock()
	if s.lookupCache == nil {
		s.lookupCache = map[string]*LookupResult{}
	}
	s.lookupCache[cacheKey] = v
	s.mu.Unlock()
	return v, nil
}
func (s *LocalService) ValidateCode(ctx context.Context, r ValidateCodeRequest) (*ValidationResult, error) {
	if r.URL != "" {
		vs, err := s.Store.GetValueSet(ctx, s.ScopeID, r.URL, r.Version)
		if err != nil {
			return nil, err
		}
		if vs == nil { // A binding may name a CodeSystem directly.
			got, err := s.Lookup(ctx, LookupRequest{System: r.Coding.System, Version: r.Coding.Version, Code: r.Coding.Code})
			if err != nil {
				return nil, err
			}
			if !got.Found {
				return &ValidationResult{Status: UnknownTerminology, Message: "terminology is not known"}, nil
			}
			if got.Concept.System != r.URL {
				return &ValidationResult{Status: Invalid, Message: "code is not in terminology"}, nil
			}
			return &ValidationResult{Status: Valid, DisplayWarning: r.Coding.Display != "" && r.Coding.Display != got.Concept.Display, Message: displayMessage(r.Coding.Display, got.Concept.Display)}, nil
		}
		ex, err := s.Expand(ctx, ExpandRequest{ScopeID: r.ScopeID, URL: r.URL, Version: r.Version})
		if err != nil {
			return nil, err
		}
		for _, c := range ex.Contains {
			if c.System == r.Coding.System && (r.Coding.Version == "" || c.Version == r.Coding.Version) && c.Code == r.Coding.Code {
				return &ValidationResult{Status: Valid, DisplayWarning: r.Coding.Display != "" && r.Coding.Display != c.Display, Message: displayMessage(r.Coding.Display, c.Display)}, nil
			}
		}
		return &ValidationResult{Status: Invalid, Message: "code is not in ValueSet"}, nil
	}
	got, err := s.Lookup(ctx, LookupRequest{System: r.Coding.System, Version: r.Coding.Version, Code: r.Coding.Code})
	if err != nil {
		return nil, err
	}
	if !got.Found {
		return &ValidationResult{Status: UnknownTerminology, Message: "CodeSystem is not known"}, nil
	}
	if !got.Concept.Active {
		return &ValidationResult{Status: Invalid, Message: "code is inactive"}, nil
	}
	return &ValidationResult{Status: Valid, DisplayWarning: r.Coding.Display != "" && r.Coding.Display != got.Concept.Display, Message: displayMessage(r.Coding.Display, got.Concept.Display)}, nil
}
func displayMessage(got, want string) string {
	if got != "" && want != "" && got != want {
		return "display does not match terminology"
	}
	return ""
}
func (s *LocalService) Expand(ctx context.Context, r ExpandRequest) (*Expansion, error) {
	cacheKey := r.URL + "|" + r.Version + fmt.Sprintf("|%d|%d", r.Offset, r.Count)
	s.mu.RLock()
	if s.expandCache != nil {
		if v, ok := s.expandCache[cacheKey]; ok {
			x := *v
			x.Contains = append([]Coding(nil), v.Contains...)
			s.mu.RUnlock()
			return &x, nil
		}
	}
	s.mu.RUnlock()
	v, err := s.Store.GetValueSet(ctx, s.ScopeID, r.URL, r.Version)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, fmt.Errorf("unknown ValueSet %q", r.URL)
	}
	ms, err := s.Store.ListValueSetMembers(ctx, s.ScopeID, v.CanonicalURL, v.Version)
	if err != nil {
		return nil, err
	}
	if len(ms) == 0 && v.ComposeJSON != "" {
		ms, err = s.compose(ctx, v.ComposeJSON, map[string]bool{v.CanonicalURL + "|" + v.Version: true})
		if err != nil {
			return nil, err
		}
	}
	max := s.MaxExpansion
	if max <= 0 {
		max = 10000
	}
	if len(ms) > max {
		return nil, fmt.Errorf("expansion too costly")
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].SystemURL+"|"+ms[i].Code < ms[j].SystemURL+"|"+ms[j].Code })
	start := r.Offset
	if start < 0 {
		start = 0
	}
	if start > len(ms) {
		start = len(ms)
	}
	end := len(ms)
	if r.Count > 0 && start+r.Count < end {
		end = start + r.Count
	}
	out := &Expansion{URL: v.CanonicalURL, Version: v.Version, Total: len(ms)}
	for _, m := range ms[start:end] {
		out.Contains = append(out.Contains, Coding{System: m.SystemURL, Version: m.SystemVersion, Code: m.Code, Display: m.Display})
	}
	s.mu.Lock()
	if s.expandCache == nil {
		s.expandCache = map[string]*Expansion{}
	}
	s.expandCache[cacheKey] = out
	s.mu.Unlock()
	return out, nil
}
func (s *LocalService) compose(ctx context.Context, raw string, seen map[string]bool) ([]store.TerminologyExpansionMemberRecord, error) {
	var c map[string]any
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, err
	}
	var out []store.TerminologyExpansionMemberRecord
	inc, _ := c["include"].([]any)
	for _, iv := range inc {
		m, _ := iv.(map[string]any)
		sys, _ := m["system"].(string)
		if cs, ok := m["concept"].([]any); ok {
			for _, cv := range cs {
				cm, _ := cv.(map[string]any)
				code, _ := cm["code"].(string)
				if code == "" {
					continue
				}
				x, _ := s.Store.LookupConcept(ctx, s.ScopeID, sys, "", code)
				display, _ := cm["display"].(string)
				if x != nil && display == "" {
					display = x.Display
				}
				version := ""
				if x != nil {
					version = x.SystemVersion
				}
				out = append(out, store.TerminologyExpansionMemberRecord{ScopeID: s.ScopeID, SystemURL: sys, SystemVersion: version, Code: code, Display: display})
			}
		}
		if vals, ok := m["valueSet"].([]any); ok {
			for _, vv := range vals {
				u, _ := vv.(string)
				v, e := s.Store.GetValueSet(ctx, s.ScopeID, u, "")
				if e != nil {
					return nil, e
				}
				if v == nil {
					continue
				}
				k := v.CanonicalURL + "|" + v.Version
				if seen[k] {
					continue
				}
				seen[k] = true
				ms, e := s.compose(ctx, v.ComposeJSON, seen)
				if e != nil {
					return nil, e
				}
				out = append(out, ms...)
			}
		}
		if sys != "" && m["concept"] == nil { // finite local CodeSystem inclusion
			cs, e := s.Store.ListResources(ctx, s.ScopeID, "CodeSystem")
			if e != nil {
				return nil, e
			}
			for _, cr := range cs {
				if cr.CanonicalURL != sys {
					continue
				}
				var rm map[string]any
				_ = json.Unmarshal(cr.ResourceJSON, &rm)
				var walk func([]any)
				walk = func(xs []any) {
					for _, vv := range xs {
						z, _ := vv.(map[string]any)
						code, _ := z["code"].(string)
						if code != "" {
							x, _ := s.Store.LookupConcept(ctx, s.ScopeID, sys, "", code)
							if x != nil {
								out = append(out, store.TerminologyExpansionMemberRecord{ScopeID: s.ScopeID, SystemURL: sys, SystemVersion: x.SystemVersion, Code: code, Display: x.Display})
							}
						}
						if sub, ok := z["concept"].([]any); ok {
							walk(sub)
						}
					}
				}
				if x, ok := rm["concept"].([]any); ok {
					walk(x)
				}
			}
		}
	}
	// Apply compose exclusions after all inclusions. Exclusions are intentionally
	// resolved by system/code so the resulting projection remains deterministic.
	if exc, ok := c["exclude"].([]any); ok {
		remove := map[string]bool{}
		for _, ev := range exc {
			em, _ := ev.(map[string]any)
			sys, _ := em["system"].(string)
			if concepts, ok := em["concept"].([]any); ok {
				for _, cv := range concepts {
					cm, _ := cv.(map[string]any)
					code, _ := cm["code"].(string)
					remove[sys+"|"+code] = true
				}
			}
		}
		filtered := out[:0]
		for _, m := range out {
			if !remove[m.SystemURL+"|"+m.Code] {
				filtered = append(filtered, m)
			}
		}
		out = filtered
	}
	return dedupe(out), nil
}
func dedupe(in []store.TerminologyExpansionMemberRecord) []store.TerminologyExpansionMemberRecord {
	m := map[string]store.TerminologyExpansionMemberRecord{}
	for _, x := range in {
		m[x.SystemURL+"|"+x.SystemVersion+"|"+x.Code] = x
	}
	out := make([]store.TerminologyExpansionMemberRecord, 0, len(m))
	for _, x := range m {
		out = append(out, x)
	}
	return out
}

// Compile parses a canonical CodeSystem or ValueSet and replaces its projection.
func Compile(ctx context.Context, st store.TerminologyStore, scope, sourceModule string, raw []byte) error {
	var r map[string]any
	if err := json.Unmarshal(raw, &r); err != nil {
		return err
	}
	typ, _ := r["resourceType"].(string)
	url, _ := r["url"].(string)
	ver, _ := r["version"].(string)
	status, _ := r["status"].(string)
	if url == "" {
		return fmt.Errorf("%s url is required", typ)
	}
	if typ == "CodeSystem" {
		return compileCS(ctx, st, scope, url, ver, r)
	}
	if typ == "ValueSet" {
		return compileVS(ctx, st, scope, url, ver, status, r)
	}
	return fmt.Errorf("unsupported terminology resource %q", typ)
}

// Install stores canonical JSON and compiles its replaceable projection. Callers
// using a transaction-scoped store can make both operations part of one write.
func Install(ctx context.Context, st store.TerminologyStore, record store.TerminologyResourceRecord) error {
	var normalized map[string]any
	if err := json.Unmarshal(record.ResourceJSON, &normalized); err != nil {
		return err
	}
	if record.CanonicalURL != "" {
		normalized["url"] = record.CanonicalURL
	}
	if record.Version != "" {
		normalized["version"] = record.Version
	}
	if record.Status != "" {
		normalized["status"] = record.Status
	}
	var err error
	var compileJSON []byte
	if compileJSON, err = json.Marshal(normalized); err != nil {
		return err
	}
	if err := Compile(ctx, st, record.ScopeID, record.SourceModule, compileJSON); err != nil {
		return err
	}
	return st.PutResource(ctx, record)
}

// Rebuild reconstructs all local terminology projections from canonical JSON.
func Rebuild(ctx context.Context, st store.TerminologyStore, scope string) error {
	resources, err := st.ListResources(ctx, scope, "")
	if err != nil {
		return err
	}
	for _, r := range resources {
		if r.ResourceType != "CodeSystem" && r.ResourceType != "ValueSet" {
			continue
		}
		if err := st.DeleteProjections(ctx, scope, r.ResourceType, r.CanonicalURL, r.Version); err != nil {
			return err
		}
		if err := Compile(ctx, st, scope, r.SourceModule, r.ResourceJSON); err != nil {
			return fmt.Errorf("rebuild %s|%s: %w", r.CanonicalURL, r.Version, err)
		}
	}
	return nil
}
func compileCS(ctx context.Context, st store.TerminologyStore, scope, url, ver string, r map[string]any) error {
	var out []store.TerminologyConceptRecord
	var walk func([]any, string)
	walk = func(xs []any, parent string) {
		for _, x := range xs {
			m, _ := x.(map[string]any)
			code, _ := m["code"].(string)
			if code == "" {
				continue
			}
			d, _ := m["display"].(string)
			def, _ := m["definition"].(string)
			active := true
			if b, ok := m["active"].(bool); ok {
				active = b
			}
			abs, _ := m["abstract"].(bool)
			out = append(out, store.TerminologyConceptRecord{ScopeID: scope, SystemURL: url, SystemVersion: ver, Code: code, Display: d, Definition: def, Active: active, Abstract: abs, ParentCode: parent})
			if cs, ok := m["concept"].([]any); ok {
				walk(cs, code)
			}
		}
	}
	if cs, ok := r["concept"].([]any); ok {
		walk(cs, "")
	}
	return st.ReplaceCodeSystem(ctx, scope, url, ver, out)
}
func compileVS(ctx context.Context, st store.TerminologyStore, scope, url, ver, status string, r map[string]any) error {
	b, _ := json.Marshal(r["compose"])
	fp := sha256.Sum256(b)
	return st.ReplaceValueSet(ctx, store.TerminologyValueSetRecord{ScopeID: scope, CanonicalURL: url, Version: ver, Status: status, ComposeJSON: string(b), ExpansionFingerprint: hex.EncodeToString(fp[:])}, nil)
}
