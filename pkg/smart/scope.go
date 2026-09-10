package smart

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// ActorClass is the SMART scope actor prefix (patient, user, or system).
type ActorClass string

const (
	ActorPatient ActorClass = "patient"
	ActorUser    ActorClass = "user"
	ActorSystem  ActorClass = "system"
)

// AccessVerb is the access capability granted by a resource scope.
type AccessVerb string

const (
	VerbRead  AccessVerb = "read"
	VerbWrite AccessVerb = "write"
	VerbAll   AccessVerb = "*"
)

// ScopeKind classifies a parsed scope token.
type ScopeKind string

const (
	// ScopeKindResource is a resource permission scope (patient|user|system).
	ScopeKindResource ScopeKind = "resource"
	// ScopeKindLaunch is a launch or launch/* context marker.
	ScopeKindLaunch ScopeKind = "launch"
	// ScopeKindSpecialty covers openid, fhirUser, offline_access, and similar.
	ScopeKindSpecialty ScopeKind = "specialty"
)

// Scope is one normalized SMART scope token.
type Scope struct {
	// Raw is the original scope string after whitespace trim.
	Raw string `json:"raw"`
	// Kind classifies the scope.
	Kind ScopeKind `json:"kind"`
	// Actor is patient, user, or system for resource scopes; empty otherwise.
	Actor ActorClass `json:"actor,omitempty"`
	// Resource is "*" or a FHIR resource type for resource scopes.
	Resource string `json:"resource,omitempty"`
	// Verb is read, write, or * for resource scopes (v1 compatibility).
	Verb AccessVerb `json:"verb,omitempty"`
	// Letters is the normalized CRUDS suffix (for example "rs", "cud", "cruds").
	Letters string `json:"letters,omitempty"`
	// Filters holds optional SMART 2.2 search-parameter filters from the scope suffix.
	Filters url.Values `json:"filters,omitempty"`
	// LaunchType is set for launch scopes ("" for bare "launch", "patient" for
	// "launch/patient", "encounter" for "launch/encounter").
	LaunchType string `json:"launchType,omitempty"`
	// Specialty is the specialty token name (openid, fhirUser, ...).
	Specialty string `json:"specialty,omitempty"`
}

// Matches reports whether this resource scope authorizes the given resource type
// and verb. Non-resource scopes never match.
func (s Scope) Matches(resourceType string, verb AccessVerb) bool {
	if s.Kind != ScopeKindResource {
		return false
	}
	if !resourceMatches(s.Resource, resourceType) {
		return false
	}
	if s.Letters != "" {
		return lettersMatchVerb(s.Letters, verb)
	}
	return verbMatches(s.Verb, verb)
}

// MatchesActor reports whether this resource scope matches actor, resource, and verb.
func (s Scope) MatchesActor(actor ActorClass, resourceType string, verb AccessVerb) bool {
	if s.Kind != ScopeKindResource || s.Actor != actor {
		return false
	}
	return s.Matches(resourceType, verb)
}

func resourceMatches(pattern, resourceType string) bool {
	if pattern == "*" {
		return true
	}
	return strings.EqualFold(pattern, resourceType)
}

func verbMatches(granted, requested AccessVerb) bool {
	if granted == VerbAll {
		return true
	}
	return granted == requested
}

func lettersMatchVerb(letters string, verb AccessVerb) bool {
	switch verb {
	case VerbRead:
		return strings.ContainsRune(letters, 'r')
	case VerbWrite:
		return strings.ContainsRune(letters, 'c') ||
			strings.ContainsRune(letters, 'u') ||
			strings.ContainsRune(letters, 'd')
	case VerbAll:
		return letters == "cruds"
	default:
		return false
	}
}

// ScopeSet is a normalized, deduplicated collection of SMART scopes.
type ScopeSet struct {
	scopes []Scope
}

// Scopes returns a copy of the normalized scopes in stable order.
func (s ScopeSet) Scopes() []Scope {
	out := make([]Scope, len(s.scopes))
	copy(out, s.scopes)
	return out
}

// Len returns the number of scopes.
func (s ScopeSet) Len() int { return len(s.scopes) }

// Empty reports whether the set has no scopes.
func (s ScopeSet) Empty() bool { return len(s.scopes) == 0 }

// Strings returns the raw scope strings in stable order.
func (s ScopeSet) Strings() []string {
	out := make([]string, len(s.scopes))
	for i, sc := range s.scopes {
		out[i] = sc.Raw
	}
	return out
}

// SpaceSeparated returns scopes joined by a single space (OAuth scope form).
func (s ScopeSet) SpaceSeparated() string {
	return strings.Join(s.Strings(), " ")
}

// Has reports whether an identical raw scope is present.
func (s ScopeSet) Has(raw string) bool {
	raw = strings.TrimSpace(raw)
	for _, sc := range s.scopes {
		if sc.Raw == raw {
			return true
		}
	}
	return false
}

// HasLaunch reports whether any launch context marker is present.
func (s ScopeSet) HasLaunch() bool {
	for _, sc := range s.scopes {
		if sc.Kind == ScopeKindLaunch {
			return true
		}
	}
	return false
}

// LaunchScopes returns launch-kind scopes.
func (s ScopeSet) LaunchScopes() []Scope {
	var out []Scope
	for _, sc := range s.scopes {
		if sc.Kind == ScopeKindLaunch {
			out = append(out, sc)
		}
	}
	return out
}

// ResourceScopes returns resource permission scopes, optionally filtered by actor.
func (s ScopeSet) ResourceScopes(actors ...ActorClass) []Scope {
	var filter map[ActorClass]struct{}
	if len(actors) > 0 {
		filter = make(map[ActorClass]struct{}, len(actors))
		for _, a := range actors {
			filter[a] = struct{}{}
		}
	}
	var out []Scope
	for _, sc := range s.scopes {
		if sc.Kind != ScopeKindResource {
			continue
		}
		if filter != nil {
			if _, ok := filter[sc.Actor]; !ok {
				continue
			}
		}
		out = append(out, sc)
	}
	return out
}

// Allows reports whether any resource scope authorizes actor/resource/verb.
func (s ScopeSet) Allows(actor ActorClass, resourceType string, verb AccessVerb) bool {
	for _, sc := range s.scopes {
		if sc.MatchesActor(actor, resourceType, verb) {
			return true
		}
	}
	return false
}

// AllowsRead is Allows(..., VerbRead).
func (s ScopeSet) AllowsRead(actor ActorClass, resourceType string) bool {
	return s.Allows(actor, resourceType, VerbRead)
}

// AllowsWrite is Allows(..., VerbWrite).
func (s ScopeSet) AllowsWrite(actor ActorClass, resourceType string) bool {
	return s.Allows(actor, resourceType, VerbWrite)
}

// ContainsAll reports whether every required raw scope string is present.
func (s ScopeSet) ContainsAll(required []string) bool {
	for _, r := range required {
		if !s.Has(r) {
			return false
		}
	}
	return true
}

// SubsetOf reports whether every scope in s is allowed by allowed (exact raw match
// or covered by a broader resource scope in allowed).
func (s ScopeSet) SubsetOf(allowed ScopeSet) bool {
	for _, sc := range s.scopes {
		if coveredBy(sc, allowed) {
			continue
		}
		return false
	}
	return true
}

func coveredBy(need Scope, allowed ScopeSet) bool {
	for _, a := range allowed.scopes {
		if a.Raw == need.Raw {
			return true
		}
		if need.Kind != ScopeKindResource || a.Kind != ScopeKindResource {
			continue
		}
		if a.Actor != need.Actor {
			continue
		}
		if !resourceMatches(a.Resource, need.Resource) && a.Resource != "*" {
			continue
		}
		// need.Resource must be covered by a.Resource
		if a.Resource != "*" && !strings.EqualFold(a.Resource, need.Resource) {
			continue
		}
		if scopeCovers(a, need) {
			return true
		}
	}
	return false
}

func verbCovers(granted, need AccessVerb) bool {
	if granted == VerbAll {
		return true
	}
	return granted == need
}

func scopeCovers(granted, need Scope) bool {
	if granted.Letters != "" && need.Letters != "" {
		if !lettersCover(granted.Letters, need.Letters) {
			return false
		}
	} else if !verbCovers(granted.Verb, need.Verb) {
		return false
	}
	if len(need.Filters) == 0 {
		return true
	}
	if len(granted.Filters) == 0 {
		return true
	}
	return filtersSubset(need.Filters, granted.Filters)
}

func filtersSubset(need, granted url.Values) bool {
	for key, needVals := range need {
		grantedVals := granted[key]
		if len(grantedVals) == 0 {
			return false
		}
		for _, needVal := range needVals {
			found := false
			for _, grantedVal := range grantedVals {
				if strings.EqualFold(needVal, grantedVal) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

// ScopeParser parses and normalizes SMART scope strings.
type ScopeParser struct{}

// NewScopeParser returns a ScopeParser.
func NewScopeParser() *ScopeParser {
	return &ScopeParser{}
}

// ParseScopes parses a space-delimited SMART scope string into a normalized ScopeSet.
func ParseScopes(raw string) (ScopeSet, error) {
	return NewScopeParser().Parse(raw)
}

// Parse parses a space-delimited SMART scope string into a normalized ScopeSet.
func (p *ScopeParser) Parse(raw string) (ScopeSet, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ScopeSet{}, nil
	}
	parts := strings.Fields(raw)
	seen := make(map[string]Scope, len(parts))
	for _, part := range parts {
		sc, err := parseOneScope(part)
		if err != nil {
			return ScopeSet{}, err
		}
		key := normalizeKey(sc)
		if existing, ok := seen[key]; ok {
			// Prefer broader verb when overlapping identical actor/resource keys.
			if sc.Kind == ScopeKindResource && existing.Kind == ScopeKindResource {
				seen[key] = mergeResourceScope(existing, sc)
			}
			continue
		}
		seen[key] = sc
	}
	out := make([]Scope, 0, len(seen))
	for _, sc := range seen {
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Raw < out[j].Raw
	})
	// Collapse overlaps: patient/Observation.read covered by patient/*.read
	out = collapseOverlaps(out)
	return ScopeSet{scopes: out}, nil
}

func normalizeKey(sc Scope) string {
	switch sc.Kind {
	case ScopeKindResource:
		return fmt.Sprintf("resource:%s:%s:%s:%s", sc.Actor, strings.ToLower(sc.Resource), sc.Letters, filterKey(sc.Filters))
	case ScopeKindLaunch:
		return "launch:" + sc.LaunchType
	case ScopeKindSpecialty:
		return "specialty:" + sc.Specialty
	default:
		return "raw:" + sc.Raw
	}
}

func mergeResourceScope(a, b Scope) Scope {
	if a.Actor != b.Actor || !strings.EqualFold(a.Resource, b.Resource) {
		return a
	}
	if !filtersEqual(a.Filters, b.Filters) {
		return a
	}
	letters := mergeLetters(a.Letters, b.Letters)
	verb := verbFromLetters(letters)
	return Scope{
		Raw:      rawFromScope(a.Actor, a.Resource, letters, a.Filters),
		Kind:     ScopeKindResource,
		Actor:    a.Actor,
		Resource: a.Resource,
		Verb:     verb,
		Letters:  letters,
		Filters:  cloneFilters(a.Filters),
	}
}

func rawFromScope(actor ActorClass, resource, letters string, filters url.Values) string {
	suffix := letters
	switch letters {
	case "rs":
		suffix = "read"
	case "cud":
		suffix = "write"
	case "cruds":
		suffix = "*"
	}
	raw := fmt.Sprintf("%s/%s.%s", actor, resource, suffix)
	if len(filters) > 0 {
		raw += "?" + filters.Encode()
	}
	return raw
}

func collapseOverlaps(in []Scope) []Scope {
	var out []Scope
	for _, sc := range in {
		if sc.Kind != ScopeKindResource {
			out = append(out, sc)
			continue
		}
		covered := false
		for _, other := range in {
			if other.Kind != ScopeKindResource || other.Raw == sc.Raw {
				continue
			}
			if other.Actor != sc.Actor {
				continue
			}
			// other is broader resource and covers verb
			if other.Resource == "*" && sc.Resource != "*" && scopeCovers(other, sc) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, sc)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Raw < out[j].Raw
	})
	return out
}

func parseOneScope(raw string) (Scope, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Scope{}, fmt.Errorf("%w: empty scope token", ErrInvalidScope)
	}

	// Specialty / identity scopes
	switch raw {
	case "openid", "fhirUser", "profile", "offline_access", "online_access":
		return Scope{Raw: raw, Kind: ScopeKindSpecialty, Specialty: raw}, nil
	}

	// Launch markers
	if raw == "launch" {
		return Scope{Raw: raw, Kind: ScopeKindLaunch}, nil
	}
	if strings.HasPrefix(raw, "launch/") {
		rest := strings.TrimPrefix(raw, "launch/")
		if rest == "" || strings.ContainsAny(rest, "/. ") {
			return Scope{}, fmt.Errorf("%w: malformed launch scope %q", ErrInvalidScope, raw)
		}
		return Scope{Raw: raw, Kind: ScopeKindLaunch, LaunchType: rest}, nil
	}

	// Resource scopes: {actor}/{resource}.{access}[?filters]
	scopePart := raw
	var filters url.Values
	if q := strings.IndexByte(raw, '?'); q >= 0 {
		scopePart = raw[:q]
		parsed, err := url.ParseQuery(raw[q+1:])
		if err != nil {
			return Scope{}, fmt.Errorf("%w: malformed scope filters in %q", ErrInvalidScope, raw)
		}
		if len(parsed) == 0 {
			return Scope{}, fmt.Errorf("%w: empty scope filters in %q", ErrInvalidScope, raw)
		}
		filters = parsed
	}

	slash := strings.IndexByte(scopePart, '/')
	if slash <= 0 || slash == len(scopePart)-1 {
		return Scope{}, fmt.Errorf("%w: unrecognized scope %q", ErrInvalidScope, raw)
	}
	actorStr := scopePart[:slash]
	rest := scopePart[slash+1:]
	actor := ActorClass(actorStr)
	switch actor {
	case ActorPatient, ActorUser, ActorSystem:
	default:
		return Scope{}, fmt.Errorf("%w: unknown actor %q in %q", ErrInvalidScope, actorStr, raw)
	}

	dot := strings.LastIndexByte(rest, '.')
	if dot <= 0 || dot == len(rest)-1 {
		return Scope{}, fmt.Errorf("%w: expected resource.verb in %q", ErrInvalidScope, raw)
	}
	resource := rest[:dot]
	verbStr := rest[dot+1:]
	if resource == "" {
		return Scope{}, fmt.Errorf("%w: empty resource in %q", ErrInvalidScope, raw)
	}
	if resource != "*" && !isValidResourceName(resource) {
		return Scope{}, fmt.Errorf("%w: invalid resource name %q in %q", ErrInvalidScope, resource, raw)
	}
	verb, letters, err := parseAccessSuffix(verbStr)
	if err != nil {
		return Scope{}, fmt.Errorf("%w: %v in %q", ErrInvalidScope, err, raw)
	}
	if actor == ActorPatient && (strings.ContainsRune(letters, 'c') ||
		strings.ContainsRune(letters, 'u') ||
		strings.ContainsRune(letters, 'd')) {
		return Scope{}, fmt.Errorf("%w: patient scopes only support read/search in %q", ErrInvalidScope, raw)
	}
	return Scope{
		Raw:      raw,
		Kind:     ScopeKindResource,
		Actor:    actor,
		Resource: resource,
		Verb:     verb,
		Letters:  letters,
		Filters:  cloneFilters(filters),
	}, nil
}

func cloneFilters(v url.Values) url.Values {
	if len(v) == 0 {
		return nil
	}
	out := make(url.Values, len(v))
	for k, vals := range v {
		out[k] = append([]string(nil), vals...)
	}
	return out
}

func filtersEqual(a, b url.Values) bool {
	return filterKey(a) == filterKey(b)
}

func isValidResourceName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if r < 'A' || r > 'Z' {
				return false
			}
			continue
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}
