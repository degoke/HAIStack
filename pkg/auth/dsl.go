package auth

import (
	"fmt"
	"strings"
)

// Effect is the effect of a policy rule.
type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

// PolicyDocument is a code-loaded policy document (JSON or YAML).
type PolicyDocument struct {
	Version       string       `json:"version" yaml:"version"`
	DefaultEffect Effect       `json:"defaultEffect,omitempty" yaml:"defaultEffect,omitempty"`
	Rules         []PolicyRule `json:"rules" yaml:"rules"`
}

// PolicyRule is one ordered allow/deny rule.
type PolicyRule struct {
	Name   string    `json:"name" yaml:"name"`
	Effect Effect    `json:"effect" yaml:"effect"`
	Match  RuleMatch `json:"match" yaml:"match"`
	Reason string    `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// RuleMatch describes the conditions a rule must satisfy. Empty slices and nil
// pointers mean "any" (do not constrain). Wildcard "*" in a string list matches
// any value for that field.
type RuleMatch struct {
	PrincipalKinds []string `json:"principalKinds,omitempty" yaml:"principalKinds,omitempty"`
	Tenants        []string `json:"tenants,omitempty" yaml:"tenants,omitempty"`
	Roles          []string `json:"roles,omitempty" yaml:"roles,omitempty"`
	AnyPermissions []string `json:"anyPermissions,omitempty" yaml:"anyPermissions,omitempty"`
	AllPermissions []string `json:"allPermissions,omitempty" yaml:"allPermissions,omitempty"`
	ResourceTypes  []string `json:"resourceTypes,omitempty" yaml:"resourceTypes,omitempty"`
	Actions        []string `json:"actions,omitempty" yaml:"actions,omitempty"`
	ViewNames      []string `json:"viewNames,omitempty" yaml:"viewNames,omitempty"`
	ToolNames      []string `json:"toolNames,omitempty" yaml:"toolNames,omitempty"`
	ModuleNames    []string `json:"moduleNames,omitempty" yaml:"moduleNames,omitempty"`
	PurposeOfUse   []string `json:"purposeOfUse,omitempty" yaml:"purposeOfUse,omitempty"`
	DeviceTrusted  *bool    `json:"deviceTrusted,omitempty" yaml:"deviceTrusted,omitempty"`
	DeviceStatuses []string `json:"deviceStatuses,omitempty" yaml:"deviceStatuses,omitempty"`
	PatientScoped  *bool    `json:"patientScoped,omitempty" yaml:"patientScoped,omitempty"`
}

// CompiledPolicy is an immutable evaluator compiled from a PolicyDocument.
type CompiledPolicy struct {
	version       string
	defaultEffect Effect
	rules         []compiledRule
}

type compiledRule struct {
	name   string
	effect Effect
	match  RuleMatch
	reason string
}

// CompilePolicy validates and compiles a policy document.
func CompilePolicy(doc PolicyDocument) (*CompiledPolicy, error) {
	if err := validatePolicyDocument(doc); err != nil {
		return nil, err
	}
	effect := doc.DefaultEffect
	if effect == "" {
		effect = EffectDeny
	}
	rules := make([]compiledRule, 0, len(doc.Rules))
	for _, r := range doc.Rules {
		rules = append(rules, compiledRule{
			name:   r.Name,
			effect: r.Effect,
			match:  cloneMatch(r.Match),
			reason: r.Reason,
		})
	}
	return &CompiledPolicy{
		version:       doc.Version,
		defaultEffect: effect,
		rules:         rules,
	}, nil
}

func validatePolicyDocument(doc PolicyDocument) error {
	if doc.Version == "" {
		return fmt.Errorf("%w: version required", ErrInvalidPolicy)
	}
	if doc.DefaultEffect != "" && doc.DefaultEffect != EffectAllow && doc.DefaultEffect != EffectDeny {
		return fmt.Errorf("%w: invalid defaultEffect %q", ErrInvalidPolicy, doc.DefaultEffect)
	}
	if len(doc.Rules) == 0 {
		return fmt.Errorf("%w: at least one rule required", ErrInvalidPolicy)
	}
	names := make(map[string]struct{}, len(doc.Rules))
	for i, r := range doc.Rules {
		if r.Name == "" {
			return fmt.Errorf("%w: rule[%d] name required", ErrInvalidPolicy, i)
		}
		if _, ok := names[r.Name]; ok {
			return fmt.Errorf("%w: duplicate rule name %q", ErrInvalidPolicy, r.Name)
		}
		names[r.Name] = struct{}{}
		if r.Effect != EffectAllow && r.Effect != EffectDeny {
			return fmt.Errorf("%w: rule %q has invalid effect %q", ErrInvalidPolicy, r.Name, r.Effect)
		}
	}
	return nil
}

func cloneMatch(m RuleMatch) RuleMatch {
	out := RuleMatch{
		PrincipalKinds: append([]string(nil), m.PrincipalKinds...),
		Tenants:        append([]string(nil), m.Tenants...),
		Roles:          append([]string(nil), m.Roles...),
		AnyPermissions: append([]string(nil), m.AnyPermissions...),
		AllPermissions: append([]string(nil), m.AllPermissions...),
		ResourceTypes:  append([]string(nil), m.ResourceTypes...),
		Actions:        append([]string(nil), m.Actions...),
		ViewNames:      append([]string(nil), m.ViewNames...),
		ToolNames:      append([]string(nil), m.ToolNames...),
		ModuleNames:    append([]string(nil), m.ModuleNames...),
		PurposeOfUse:   append([]string(nil), m.PurposeOfUse...),
		DeviceStatuses: append([]string(nil), m.DeviceStatuses...),
	}
	if m.DeviceTrusted != nil {
		v := *m.DeviceTrusted
		out.DeviceTrusted = &v
	}
	if m.PatientScoped != nil {
		v := *m.PatientScoped
		out.PatientScoped = &v
	}
	return out
}

// Evaluate applies rules in order. The first matching rule wins. If none match,
// DefaultEffect applies (deny by default).
func (p *CompiledPolicy) Evaluate(in evalInput) Decision {
	for _, rule := range p.rules {
		if !ruleMatches(rule.match, in) {
			continue
		}
		reason := rule.reason
		if reason == "" {
			reason = fmt.Sprintf("matched rule %q", rule.name)
		}
		if rule.effect == EffectAllow {
			d := Allow(reason)
			d.RequiredPermissions = append([]string(nil), in.RequiredPermissions...)
			return d
		}
		return Deny(reason)
	}
	if p.defaultEffect == EffectAllow {
		return Allow("default allow")
	}
	return Deny("denied by default: no matching allow rule")
}

func ruleMatches(m RuleMatch, in evalInput) bool {
	if !matchStringList(m.PrincipalKinds, string(in.Principal.Kind)) {
		return false
	}
	if !matchStringList(m.Tenants, in.Tenant.TenantID) {
		return false
	}
	if len(m.Roles) > 0 && !anyOverlap(m.Roles, in.Roles) {
		return false
	}
	if len(m.AnyPermissions) > 0 && !hasAnyPermission(in.Permissions, m.AnyPermissions) {
		return false
	}
	if len(m.AllPermissions) > 0 && !hasAllPermissions(in.Permissions, m.AllPermissions) {
		return false
	}
	if !matchStringList(m.ResourceTypes, in.ResourceType) {
		return false
	}
	if !matchStringList(m.Actions, in.Action) {
		return false
	}
	if !matchStringList(m.ViewNames, in.ViewName) {
		return false
	}
	if !matchStringList(m.ToolNames, in.ToolName) {
		return false
	}
	if !matchStringList(m.ModuleNames, in.ModuleName) {
		return false
	}
	if !matchStringList(m.PurposeOfUse, in.Tenant.PurposeOfUse) {
		return false
	}
	if m.DeviceTrusted != nil {
		trusted := in.Device != nil && in.Device.Trusted
		if trusted != *m.DeviceTrusted {
			return false
		}
	}
	if len(m.DeviceStatuses) > 0 {
		status := ""
		if in.Device != nil {
			status = in.Device.Status
		}
		if !matchStringList(m.DeviceStatuses, status) {
			return false
		}
	}
	if m.PatientScoped != nil {
		scoped := in.Tenant.PatientScope != ""
		if scoped != *m.PatientScoped {
			return false
		}
	}
	return true
}

// matchStringList returns true when the constraint list is empty (any), the
// value is matched exactly, or the list contains "*".
func matchStringList(list []string, value string) bool {
	if len(list) == 0 {
		return true
	}
	for _, item := range list {
		if item == "*" || item == value {
			return true
		}
	}
	return false
}

func anyOverlap(want, have []string) bool {
	set := make(map[string]struct{}, len(have))
	for _, h := range have {
		set[h] = struct{}{}
	}
	for _, w := range want {
		if w == "*" {
			return len(have) > 0
		}
		if _, ok := set[w]; ok {
			return true
		}
	}
	return false
}

func hasAnyPermission(have []Permission, want []string) bool {
	set := permissionSet(have)
	for _, w := range want {
		if w == "*" && len(have) > 0 {
			return true
		}
		if _, ok := set[Permission(w)]; ok {
			return true
		}
		// Also compare normalized forms so "appointment.read" and
		// "read-appointment" can coexist across callers.
		if _, ok := set[Permission(normalizePermission(w))]; ok {
			return true
		}
		for h := range set {
			if permissionsEquivalent(string(h), w) {
				return true
			}
		}
	}
	return false
}

func hasAllPermissions(have []Permission, want []string) bool {
	for _, w := range want {
		if !hasAnyPermission(have, []string{w}) {
			return false
		}
	}
	return true
}

func permissionSet(perms []Permission) map[Permission]struct{} {
	set := make(map[Permission]struct{}, len(perms))
	for _, p := range perms {
		set[p] = struct{}{}
	}
	return set
}

func normalizePermission(p string) string {
	return strings.ToLower(strings.TrimSpace(p))
}

// permissionsEquivalent treats "appointment.read" and "read-appointment" as
// equal when the two segments match after delimiter normalization.
func permissionsEquivalent(a, b string) bool {
	a = normalizePermission(a)
	b = normalizePermission(b)
	if a == b {
		return true
	}
	ra := splitPermission(a)
	rb := splitPermission(b)
	if len(ra) != 2 || len(rb) != 2 {
		return false
	}
	return (ra[0] == rb[0] && ra[1] == rb[1]) || (ra[0] == rb[1] && ra[1] == rb[0])
}

func splitPermission(p string) []string {
	if i := strings.IndexByte(p, '.'); i >= 0 {
		return []string{p[:i], p[i+1:]}
	}
	if i := strings.IndexByte(p, '-'); i >= 0 {
		return []string{p[:i], p[i+1:]}
	}
	return []string{p}
}
