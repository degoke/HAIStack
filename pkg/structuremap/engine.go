package structuremap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
)

// Engine executes StructureMap group rules.
type Engine struct {
	FHIRPath fhirpath.Engine
	// Strict reports an error when a rule declares sources but none match.
	Strict bool
}

// ExecuteInput names resources available to the map by variable name.
type ExecuteInput map[string]any

// Execute runs a StructureMap and returns produced FHIR resources.
func (e Engine) Execute(ctx context.Context, m Map, inputs ExecuteInput) ([]json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	group, err := entryGroup(m)
	if err != nil {
		return nil, err
	}
	vars := map[string]any{"__root": inputs}
	for _, in := range group.Input {
		if value, ok := inputs[in.Name]; ok {
			vars[in.Name] = value
			continue
		}
		if in.Mode == "target" && in.Type != "" {
			vars[in.Name] = newTypedInstance(in.Type)
		}
	}
	if err := e.executeRules(ctx, m, group.Rule, vars); err != nil {
		return nil, err
	}
	return collectOutputs(group.Input, vars)
}

func entryGroup(m Map) (*Group, error) {
	if len(m.Group) == 0 {
		return nil, fmt.Errorf("StructureMap %s has no groups", m.URL)
	}
	if m.Name != "" {
		for i := range m.Group {
			if m.Group[i].Name == m.Name {
				return &m.Group[i], nil
			}
		}
	}
	return &m.Group[0], nil
}

func (e Engine) executeRules(ctx context.Context, m Map, rules []Rule, vars map[string]any) error {
	for _, rule := range rules {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.executeRule(ctx, m, rule, vars); err != nil {
			if rule.Name != "" {
				return fmt.Errorf("rule %q: %w", rule.Name, err)
			}
			return err
		}
	}
	return nil
}

func (e Engine) executeRule(ctx context.Context, m Map, rule Rule, vars map[string]any) error {
	sourceBindings, err := e.bindSources(ctx, rule.Source, vars)
	if err != nil {
		return err
	}
	if len(sourceBindings) == 0 && len(rule.Source) > 0 {
		if e.Strict || requiresSourceMatch(rule.Source) {
			name := rule.Name
			if name == "" {
				name = "unnamed"
			}
			return fmt.Errorf("rule %q: no source bindings matched", name)
		}
		return nil
	}
	iterations := 1
	if len(sourceBindings) > 0 {
		iterations = len(sourceBindings)
	}
	for i := 0; i < iterations; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		scope := cloneVars(vars)
		if len(sourceBindings) > 0 {
			for name, values := range sourceBindings[i] {
				scope[name] = values
			}
		}
		if err := e.applyTargets(ctx, rule.Target, scope, firstSourceValue(sourceBindings, i)); err != nil {
			return err
		}
		if len(rule.Rule) > 0 {
			if err := e.executeRules(ctx, m, rule.Rule, scope); err != nil {
				return err
			}
		}
		for _, dep := range rule.Dependent {
			if err := e.invokeGroup(ctx, m, dep, scope); err != nil {
				return err
			}
		}
		for key, value := range scope {
			if key == "__root" {
				continue
			}
			vars[key] = value
		}
	}
	return nil
}

func requiresSourceMatch(sources []Source) bool {
	for _, source := range sources {
		if source.Min > 0 {
			return true
		}
	}
	return false
}

func (e Engine) bindSources(ctx context.Context, sources []Source, vars map[string]any) ([]map[string]any, error) {
	if len(sources) == 0 {
		return nil, nil
	}
	var combinations []map[string]any
	if err := e.walkSources(ctx, sources, 0, vars, map[string]any{}, &combinations); err != nil {
		return nil, err
	}
	return combinations, nil
}

func (e Engine) walkSources(ctx context.Context, sources []Source, index int, vars map[string]any, current map[string]any, out *[]map[string]any) error {
	if index >= len(sources) {
		if len(current) > 0 {
			*out = append(*out, cloneStringAnyMap(current))
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	source := sources[index]
	base, ok := vars[source.Context]
	if !ok {
		return nil
	}
	candidates := navigateElements(base, source.Element)
	if len(candidates) == 0 && len(source.Element) == 0 {
		candidates = []any{base}
	}
	filtered := make([]any, 0, len(candidates))
	for _, candidate := range candidates {
		if source.Condition != "" && !e.evaluateCondition(ctx, candidate, source.Condition, vars) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	filtered, err := applySourceListMode(filtered, source.ListMode)
	if err != nil {
		return err
	}
	for _, candidate := range filtered {
		if source.Variable != "" {
			current[source.Variable] = candidate
		}
		if err := e.walkSources(ctx, sources, index+1, vars, current, out); err != nil {
			return err
		}
		if source.Variable != "" {
			delete(current, source.Variable)
		}
	}
	return nil
}

func applySourceListMode(candidates []any, listMode string) ([]any, error) {
	switch strings.ToLower(strings.TrimSpace(listMode)) {
	case "", "no_list":
		return candidates, nil
	case "first":
		if len(candidates) == 0 {
			return candidates, nil
		}
		return []any{candidates[0]}, nil
	case "last":
		if len(candidates) == 0 {
			return candidates, nil
		}
		return []any{candidates[len(candidates)-1]}, nil
	case "only_one":
		if len(candidates) > 1 {
			return nil, fmt.Errorf("source listMode only_one matched %d candidates", len(candidates))
		}
		return candidates, nil
	case "not_first":
		if len(candidates) <= 1 {
			return nil, nil
		}
		return candidates[1:], nil
	case "not_last":
		if len(candidates) <= 1 {
			return nil, nil
		}
		return candidates[:len(candidates)-1], nil
	default:
		return candidates, nil
	}
}

func (e Engine) evaluateCondition(ctx context.Context, value any, condition string, vars map[string]any) bool {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true
	}
	if e.FHIRPath == nil {
		return evaluateSimpleCondition(asMap(value), condition)
	}
	env := cloneStringAnyMap(vars)
	values, err := e.FHIRPath.EvalWithEnv(ctx, condition, value, env)
	if err != nil {
		return evaluateSimpleCondition(asMap(value), condition)
	}
	for _, v := range values {
		if truthy(v.Raw()) {
			return true
		}
	}
	return false
}

func evaluateSimpleCondition(object map[string]any, condition string) bool {
	condition = strings.TrimSpace(condition)
	if strings.Contains(condition, "=") {
		parts := strings.SplitN(condition, "=", 2)
		left := strings.TrimSpace(parts[0])
		right := strings.Trim(strings.TrimSpace(parts[1]), "'\"")
		if value, ok := object[left]; ok {
			return fmt.Sprint(value) == right
		}
	}
	return false
}

func (e Engine) applyTargets(ctx context.Context, targets []Target, vars map[string]any, sourceValue any) error {
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		value, err := e.resolveTargetValue(ctx, target, vars, sourceValue)
		if err != nil {
			return err
		}
		if target.Variable != "" {
			vars[target.Variable] = value
		}
		if len(target.Element) == 0 {
			if target.Variable == "" && target.Transform != "" {
				vars[target.Context] = value
			}
			continue
		}
		root, ok := vars[target.Context].(map[string]any)
		if !ok {
			return fmt.Errorf("target context %q is not an object", target.Context)
		}
		if err := assignElementValue(root, target.Element, value, target.ListMode); err != nil {
			return err
		}
	}
	return nil
}

func (e Engine) resolveTargetValue(ctx context.Context, target Target, vars map[string]any, sourceValue any) (any, error) {
	if target.Transform != "" {
		return applyTransform(ctx, e.FHIRPath, target.Transform, target.Parameter, vars, sourceContextValue(target, vars, sourceValue))
	}
	if len(target.Parameter) > 0 {
		return resolveParameter(target.Parameter[0], vars)
	}
	if sourceValue != nil {
		return cloneValue(sourceValue), nil
	}
	return map[string]any{}, nil
}

func (e Engine) invokeGroup(ctx context.Context, m Map, dep Dependent, vars map[string]any) error {
	group := findGroup(m, dep.Name)
	if group == nil {
		return fmt.Errorf("StructureMap group %q not found", dep.Name)
	}
	scope := map[string]any{"__root": vars["__root"]}
	if len(dep.Variable) > 0 {
		for _, name := range dep.Variable {
			if value, ok := vars[name]; ok {
				scope[name] = value
			}
		}
	} else {
		for _, in := range group.Input {
			if value, ok := vars[in.Name]; ok {
				scope[in.Name] = value
			} else if in.Mode == "target" && in.Type != "" {
				scope[in.Name] = newTypedInstance(in.Type)
			}
		}
	}
	if err := e.executeRules(ctx, m, group.Rule, scope); err != nil {
		return err
	}
	for key, value := range scope {
		if key == "__root" {
			continue
		}
		vars[key] = value
	}
	return nil
}

func findGroup(m Map, name string) *Group {
	for i := range m.Group {
		if m.Group[i].Name == name {
			return &m.Group[i]
		}
	}
	return nil
}

func collectOutputs(inputs []Input, vars map[string]any) ([]json.RawMessage, error) {
	var out []json.RawMessage
	for _, in := range inputs {
		if in.Mode != "target" {
			continue
		}
		value, ok := vars[in.Name]
		if !ok {
			continue
		}
		resources := extractResources(value)
		for _, resource := range resources {
			raw, err := json.Marshal(resource)
			if err != nil {
				return nil, err
			}
			out = append(out, raw)
		}
	}
	return out, nil
}

func extractResources(value any) []map[string]any {
	switch v := value.(type) {
	case map[string]any:
		if rt, ok := v["resourceType"].(string); ok && rt == "Bundle" {
			var resources []map[string]any
			entries, _ := v["entry"].([]any)
			for _, entry := range entries {
				entryMap, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				if resource, ok := entryMap["resource"].(map[string]any); ok {
					resources = append(resources, resource)
				}
			}
			if len(resources) > 0 {
				return resources
			}
		}
		if _, ok := v["resourceType"].(string); ok {
			return []map[string]any{v}
		}
		return nil
	default:
		return nil
	}
}

func firstSourceValue(bindings []map[string]any, index int) any {
	if len(bindings) == 0 || index >= len(bindings) {
		return nil
	}
	for _, value := range bindings[index] {
		return value
	}
	return nil
}

func cloneVars(vars map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range vars {
		out[key] = value
	}
	return out
}

func cloneStringAnyMap(vars map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range vars {
		out[key] = value
	}
	return out
}

func hasListMode(modes []string, want string) bool {
	for _, mode := range modes {
		if strings.EqualFold(mode, want) {
			return true
		}
	}
	return false
}

func asMap(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return map[string]any{}
}

func sourceContextValue(target Target, vars map[string]any, sourceValue any) any {
	if sourceValue != nil {
		return sourceValue
	}
	if value, ok := vars[target.Context]; ok {
		return value
	}
	return sourceValue
}

func truthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case float64:
		return v != 0
	case int:
		return v != 0
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	default:
		return true
	}
}
