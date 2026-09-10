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
}

// ExecuteInput names resources available to the map by variable name.
type ExecuteInput map[string]any

// Execute runs a StructureMap and returns produced FHIR resources.
func (e Engine) Execute(ctx context.Context, m Map, inputs ExecuteInput) ([]json.RawMessage, error) {
	if len(m.Group) == 0 {
		return nil, fmt.Errorf("StructureMap %s has no groups", m.URL)
	}
	group := m.Group[0]
	vars := map[string]any{"__root": inputs}
	for _, in := range group.Input {
		if value, ok := inputs[in.Name]; ok {
			vars[in.Name] = value
			continue
		}
		if in.Mode == "target" && in.Type != "" {
			typeName := resourceTypeName(in.Type)
			if isFHIRResourceType(typeName) {
				vars[in.Name] = map[string]any{"resourceType": typeName}
				continue
			}
			vars[in.Name] = map[string]any{}
		}
	}
	if err := e.executeRules(ctx, m, group.Rule, vars); err != nil {
		return nil, err
	}
	return collectOutputs(group.Input, vars)
}

func (e Engine) executeRules(ctx context.Context, m Map, rules []Rule, vars map[string]any) error {
	for _, rule := range rules {
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
	sourceBindings := e.bindSources(ctx, rule.Source, vars)
	if len(sourceBindings) == 0 && len(rule.Source) > 0 {
		return nil
	}
	iterations := 1
	if len(sourceBindings) > 0 {
		iterations = len(sourceBindings)
	}
	for i := 0; i < iterations; i++ {
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

func (e Engine) bindSources(ctx context.Context, sources []Source, vars map[string]any) []map[string]any {
	if len(sources) == 0 {
		return nil
	}
	var combinations []map[string]any
	e.walkSources(ctx, sources, 0, vars, map[string]any{}, &combinations)
	return combinations
}

func (e Engine) walkSources(ctx context.Context, sources []Source, index int, vars map[string]any, current map[string]any, out *[]map[string]any) {
	if index >= len(sources) {
		if len(current) > 0 {
			*out = append(*out, cloneStringAnyMap(current))
		}
		return
	}
	source := sources[index]
	base, ok := vars[source.Context]
	if !ok {
		return
	}
	candidates := navigateElements(base, source.Element)
	if len(candidates) == 0 && len(source.Element) == 0 {
		candidates = []any{base}
	}
	for _, candidate := range candidates {
		if source.Condition != "" && !e.evaluateCondition(ctx, candidate, source.Condition, vars) {
			continue
		}
		if source.Variable != "" {
			current[source.Variable] = candidate
		}
		e.walkSources(ctx, sources, index+1, vars, current, out)
		if source.Variable != "" {
			delete(current, source.Variable)
		}
	}
}

func (e Engine) evaluateCondition(ctx context.Context, value any, condition string, vars map[string]any) bool {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true
	}
	if e.FHIRPath == nil {
		if object, ok := value.(map[string]any); ok {
			return evaluateSimpleCondition(object, condition)
		}
		return false
	}
	env := cloneStringAnyMap(vars)
	for key, item := range env {
		env[key] = item
	}
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
		if hasListMode(target.ListMode, "share") || hasListMode(target.ListMode, "collate") {
			if err := appendElementPath(root, target.Element, value); err != nil {
				return err
			}
			continue
		}
		if err := setElementPath(root, target.Element, value); err != nil {
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
			}
		}
	}
	return e.executeRules(ctx, m, group.Rule, scope)
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
