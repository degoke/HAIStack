package structuremap

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/google/uuid"
)

func applyTransform(ctx context.Context, engine fhirpath.Engine, transform string, params []Parameter, vars map[string]any, sourceValue any) (any, error) {
	switch strings.ToLower(strings.TrimSpace(transform)) {
	case "", "copy":
		if len(params) == 0 {
			return unwrapFHIRValue(cloneValue(sourceValue)), nil
		}
		value, err := resolveParameter(params[0], vars)
		if err != nil {
			return nil, err
		}
		return unwrapFHIRValue(value), nil
	case "create":
		if len(params) == 0 {
			return map[string]any{}, nil
		}
		typeName, err := parameterLiteral(params[0], vars)
		if err != nil {
			return nil, err
		}
		return newTypedInstance(typeName), nil
	case "uuid":
		return uuid.NewString(), nil
	case "cc":
		coding := map[string]any{}
		if len(params) > 0 {
			if system, err := parameterLiteral(params[0], vars); err == nil && system != "" {
				coding["system"] = system
			}
		}
		if len(params) > 1 {
			if code, err := parameterLiteral(params[1], vars); err == nil && code != "" {
				coding["code"] = code
			}
		}
		if len(params) > 2 {
			if display, err := parameterLiteral(params[2], vars); err == nil && display != "" {
				coding["display"] = display
			}
		}
		return coding, nil
	case "c":
		if len(params) == 0 {
			return map[string]any{}, nil
		}
		code, err := parameterLiteral(params[0], vars)
		if err != nil {
			return nil, err
		}
		return map[string]any{"code": code}, nil
	case "evaluate":
		if len(params) == 0 {
			return nil, fmt.Errorf("evaluate transform requires a FHIRPath expression parameter")
		}
		expr, err := parameterLiteral(params[0], vars)
		if err != nil {
			return nil, err
		}
		expr = strings.TrimPrefix(expr, "%")
		if engine == nil {
			if value, ok := evaluateSimplePath(sourceValue, expr); ok {
				return value, nil
			}
			return nil, fmt.Errorf("evaluate %q: FHIRPath engine is unavailable", expr)
		}
		resource := sourceValue
		if resource == nil {
			resource = vars["__root"]
		}
		values, err := engine.EvalWithEnv(ctx, expr, resource, vars)
		if err != nil {
			if value, ok := evaluateSimplePath(sourceValue, expr); ok {
				return value, nil
			}
			return nil, fmt.Errorf("evaluate %q: %w", expr, err)
		}
		if len(values) == 0 {
			return nil, nil
		}
		return values[0].Raw(), nil
	case "id":
		if sourceValue == nil {
			return nil, nil
		}
		if object, ok := sourceValue.(map[string]any); ok {
			return object["id"], nil
		}
		return nil, nil
	case "append":
		if len(params) < 2 {
			return nil, fmt.Errorf("append transform requires source and suffix parameters")
		}
		left, err := resolveParameter(params[0], vars)
		if err != nil {
			return nil, err
		}
		right, err := parameterLiteral(params[1], vars)
		if err != nil {
			return nil, err
		}
		return fmt.Sprint(left) + right, nil
	case "cast":
		if len(params) == 0 {
			return sourceValue, nil
		}
		targetType, err := parameterLiteral(params[0], vars)
		if err != nil {
			return nil, err
		}
		return castValue(sourceValue, targetType)
	case "reference":
		refType := ""
		refID := ""
		if len(params) > 0 {
			refType, _ = parameterLiteral(params[0], vars)
		}
		if len(params) > 1 {
			refID, _ = parameterLiteral(params[1], vars)
		}
		if refID == "" && sourceValue != nil {
			refID = fmt.Sprint(sourceValue)
		}
		reference := refID
		if refType != "" && !strings.Contains(reference, "/") {
			reference = refType + "/" + reference
		}
		return map[string]any{"reference": reference}, nil
	case "translate":
		coding := map[string]any{}
		if len(params) > 0 {
			if system, err := parameterLiteral(params[0], vars); err == nil {
				coding["system"] = system
			}
		}
		if len(params) > 1 {
			if code, err := parameterLiteral(params[1], vars); err == nil {
				coding["code"] = code
			}
		}
		if len(params) > 2 {
			if display, err := parameterLiteral(params[2], vars); err == nil {
				coding["display"] = display
			}
		}
		if len(coding) == 0 && sourceValue != nil {
			return cloneValue(sourceValue), nil
		}
		return coding, nil
	default:
		return nil, fmt.Errorf("unsupported StructureMap transform %q", transform)
	}
}

func resolveParameter(param Parameter, vars map[string]any) (any, error) {
	if param.ValueID != "" {
		if value, ok := vars[param.ValueID]; ok {
			return cloneValue(value), nil
		}
		return nil, fmt.Errorf("StructureMap variable %q is not defined", param.ValueID)
	}
	return parameterLiteral(param, vars)
}

func parameterLiteral(param Parameter, vars map[string]any) (string, error) {
	switch {
	case param.ValueString != "":
		value := param.ValueString
		if strings.HasPrefix(value, "%") {
			name := strings.TrimPrefix(value, "%")
			if v, ok := vars[name]; ok {
				return fmt.Sprint(v), nil
			}
			return "", fmt.Errorf("StructureMap variable %q is not defined", name)
		}
		return value, nil
	case param.ValueID != "":
		if v, ok := vars[param.ValueID]; ok {
			return fmt.Sprint(v), nil
		}
		return "", fmt.Errorf("StructureMap variable %q is not defined", param.ValueID)
	case param.ValueBoolean != nil:
		return fmt.Sprint(*param.ValueBoolean), nil
	case param.ValueInteger != nil:
		return fmt.Sprint(*param.ValueInteger), nil
	case param.ValueDecimal != nil:
		return fmt.Sprint(*param.ValueDecimal), nil
	default:
		return "", nil
	}
}

func unwrapFHIRValue(value any) any {
	object, ok := value.(map[string]any)
	if !ok {
		return value
	}
	for key, item := range object {
		if strings.HasPrefix(key, "value") {
			return item
		}
	}
	return value
}

func castValue(value any, targetType string) (any, error) {
	switch strings.ToLower(resourceTypeName(targetType)) {
	case "string":
		return fmt.Sprint(value), nil
	case "integer", "positiveint", "unsignedint":
		switch v := value.(type) {
		case int:
			return v, nil
		case float64:
			return int(v), nil
		case string:
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, err
			}
			return n, nil
		default:
			return nil, fmt.Errorf("cannot cast %T to integer", value)
		}
	case "decimal":
		switch v := value.(type) {
		case float64:
			return v, nil
		case int:
			return float64(v), nil
		case string:
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, err
			}
			return n, nil
		default:
			return nil, fmt.Errorf("cannot cast %T to decimal", value)
		}
	case "boolean":
		switch v := value.(type) {
		case bool:
			return v, nil
		case string:
			return strings.EqualFold(v, "true"), nil
		default:
			return nil, fmt.Errorf("cannot cast %T to boolean", value)
		}
	default:
		return value, nil
	}
}

func evaluateSimplePath(value any, expr string) (any, bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, false
	}
	current := value
	for _, part := range strings.Split(expr, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasSuffix(part, "()") {
			part = strings.TrimSuffix(part, "()")
		}
		switch part {
		case "first":
			items := flattenValue(current)
			if len(items) == 0 {
				return nil, false
			}
			current = items[0]
			continue
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
