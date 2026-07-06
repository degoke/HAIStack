package conflict

import "strings"

// RuleSemantics describes how a rule treats changes at a matched path.
type RuleSemantics string

const (
	RuleSemanticsAutoMerge  RuleSemantics = "auto_merge"
	RuleSemanticsAppendOnly RuleSemantics = "append_only"
	RuleSemanticsReviewOnly RuleSemantics = "review_only"
	RuleSemanticsBlock      RuleSemantics = "block"
)

// Rule is one resource/path-specific merge policy.
type Rule struct {
	ResourceType string
	PathPrefix   string
	Semantics    RuleSemantics
	Description  string
}

// RulePack is a named collection of rules. Packs can be registered in a
// PolicyRegistry and selected per engine configuration.
type RulePack struct {
	Name  string
	Rules []Rule
}

// PolicyRegistry holds rule packs and resolves the active pack for a path.
type PolicyRegistry struct {
	packs    map[string]RulePack
	selected string
}

// DefaultPolicyRegistry returns a registry with the built-in strict safe-list pack.
func DefaultPolicyRegistry() *PolicyRegistry {
	r := &PolicyRegistry{packs: make(map[string]RulePack)}
	r.Register(DefaultRulePack())
	r.Select(DefaultRulePack().Name)
	return r
}

// Register adds a rule pack to the registry. Registering the same name replaces it.
func (r *PolicyRegistry) Register(pack RulePack) {
	if r.packs == nil {
		r.packs = make(map[string]RulePack)
	}
	r.packs[pack.Name] = pack
}

// Select chooses the active rule pack by name.
func (r *PolicyRegistry) Select(name string) {
	r.selected = name
}

// Match returns the first rule whose resource type and path prefix match.
func (r *PolicyRegistry) Match(resourceType, path string) *Rule {
	pack, ok := r.packs[r.selected]
	if !ok {
		return nil
	}
	for _, rule := range pack.Rules {
		if rule.ResourceType != resourceType {
			continue
		}
		if pathPrefixMatch(rule.PathPrefix, path) {
			copy := rule
			return &copy
		}
	}
	return nil
}

func pathPrefixMatch(prefix, path string) bool {
	if path == prefix {
		return true
	}
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	return strings.HasPrefix(path[len(prefix):], ".")
}
