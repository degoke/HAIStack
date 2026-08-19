package smart

import (
	"fmt"
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
	// Verb is read, write, or * for resource scopes.
	Verb AccessVerb `json:"verb,omitempty"`
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
		if verbCovers(a.Verb, need.Verb) {
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
		return fmt.Sprintf("resource:%s:%s", sc.Actor, strings.ToLower(sc.Resource))
	case ScopeKindLaunch:
		return "launch:" + sc.LaunchType
	case ScopeKindSpecialty:
		return "specialty:" + sc.Specialty
	default:
		return "raw:" + sc.Raw
	}
}

func mergeResourceScope(a, b Scope) Scope {
	// Same actor+resource with different verbs → widen to *
	if a.Actor == b.Actor && strings.EqualFold(a.Resource, b.Resource) {
		if a.Verb == VerbAll || b.Verb == VerbAll || a.Verb != b.Verb {
			return Scope{
				Raw:      fmt.Sprintf("%s/%s.*", a.Actor, a.Resource),
				Kind:     ScopeKindResource,
				Actor:    a.Actor,
				Resource: a.Resource,
				Verb:     VerbAll,
			}
		}
	}
	return a
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
			if other.Resource == "*" && sc.Resource != "*" && verbCovers(other.Verb, sc.Verb) {
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

	// Resource scopes: {actor}/{resource}.{verb}
	slash := strings.IndexByte(raw, '/')
	if slash <= 0 || slash == len(raw)-1 {
		return Scope{}, fmt.Errorf("%w: unrecognized scope %q", ErrInvalidScope, raw)
	}
	actorStr := raw[:slash]
	rest := raw[slash+1:]
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
	verb, err := parseVerb(verbStr)
	if err != nil {
		return Scope{}, fmt.Errorf("%w: %v in %q", ErrInvalidScope, err, raw)
	}
	if actor == ActorPatient && verb != VerbRead {
		return Scope{}, fmt.Errorf("%w: patient scopes only support read in %q", ErrInvalidScope, raw)
	}
	return Scope{
		Raw:      raw,
		Kind:     ScopeKindResource,
		Actor:    actor,
		Resource: resource,
		Verb:     verb,
	}, nil
}

func parseVerb(v string) (AccessVerb, error) {
	switch v {
	case "read":
		return VerbRead, nil
	case "write":
		return VerbWrite, nil
	case "*":
		return VerbAll, nil
	default:
		return "", fmt.Errorf("unknown access verb %q", v)
	}
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
