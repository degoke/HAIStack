package smart

import (
	"fmt"
	"sort"
	"strings"
)

// AccessOp is one SMART 2.x CRUDS operation letter.
type AccessOp rune

const (
	OpRead   AccessOp = 'r'
	OpSearch AccessOp = 's'
	OpCreate AccessOp = 'c'
	OpUpdate AccessOp = 'u'
	OpDelete AccessOp = 'd'
)

var crudsOrder = []AccessOp{OpCreate, OpRead, OpUpdate, OpDelete, OpSearch}

// AllowsOp reports whether the scope grants the requested CRUDS operation.
func (s Scope) AllowsOp(op AccessOp) bool {
	if s.Kind != ScopeKindResource {
		return false
	}
	return strings.ContainsRune(s.Letters, rune(op))
}

// MatchesOp reports whether this resource scope authorizes actor, resource type, and op.
func (s Scope) MatchesOp(actor ActorClass, resourceType string, op AccessOp) bool {
	if s.Kind != ScopeKindResource || s.Actor != actor {
		return false
	}
	if !resourceMatches(s.Resource, resourceType) {
		return false
	}
	return s.AllowsOp(op)
}

// AllowsOp reports whether any scope authorizes actor/resource/op.
func (s ScopeSet) AllowsOp(actor ActorClass, resourceType string, op AccessOp) bool {
	for _, sc := range s.scopes {
		if sc.MatchesOp(actor, resourceType, op) {
			return true
		}
	}
	return false
}

// AllowsSearch reports whether any scope authorizes search for actor/resource.
func (s ScopeSet) AllowsSearch(actor ActorClass, resourceType string) bool {
	return s.AllowsOp(actor, resourceType, OpSearch)
}

// ScopesAllowingOp returns resource scopes that authorize the operation.
func (s ScopeSet) ScopesAllowingOp(actor ActorClass, resourceType string, op AccessOp) []Scope {
	var out []Scope
	for _, sc := range s.scopes {
		if sc.MatchesOp(actor, resourceType, op) {
			out = append(out, sc)
		}
	}
	return out
}

func parseAccessSuffix(v string) (AccessVerb, string, error) {
	switch v {
	case "read":
		return VerbRead, "rs", nil
	case "write":
		return VerbWrite, "cud", nil
	case "*":
		return VerbAll, "cruds", nil
	default:
		return parseCRUDS(v)
	}
}

func parseCRUDS(v string) (AccessVerb, string, error) {
	if v == "" {
		return "", "", fmt.Errorf("empty CRUDS suffix")
	}
	seen := make(map[AccessOp]struct{}, len(v))
	for _, ch := range v {
		op := AccessOp(ch)
		switch op {
		case OpRead, OpSearch, OpCreate, OpUpdate, OpDelete:
			seen[op] = struct{}{}
		default:
			return "", "", fmt.Errorf("unknown CRUDS letter %q", ch)
		}
	}
	letters := canonicalCRUDS(seen)
	return verbFromLetters(letters), letters, nil
}

func canonicalCRUDS(seen map[AccessOp]struct{}) string {
	var out []byte
	for _, op := range crudsOrder {
		if _, ok := seen[op]; ok {
			out = append(out, byte(op))
		}
	}
	return string(out)
}

func verbFromLetters(letters string) AccessVerb {
	switch letters {
	case "rs":
		return VerbRead
	case "cud":
		return VerbWrite
	case "cruds":
		return VerbAll
	default:
		if strings.ContainsRune(letters, 'c') ||
			strings.ContainsRune(letters, 'u') ||
			strings.ContainsRune(letters, 'd') {
			if strings.ContainsRune(letters, 'r') || strings.ContainsRune(letters, 's') {
				return VerbAll
			}
			return VerbWrite
		}
		return VerbRead
	}
}

func lettersCover(granted, need string) bool {
	for _, ch := range need {
		if !strings.ContainsRune(granted, ch) {
			return false
		}
	}
	return true
}

func mergeLetters(a, b string) string {
	seen := make(map[AccessOp]struct{})
	for _, letters := range []string{a, b} {
		for _, ch := range letters {
			seen[AccessOp(ch)] = struct{}{}
		}
	}
	return canonicalCRUDS(seen)
}

func filterKey(filters map[string][]string) string {
	if len(filters) == 0 {
		return ""
	}
	keys := make([]string, 0, len(filters))
	for k := range filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vals := append([]string(nil), filters[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, "&")
}
