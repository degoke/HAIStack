package structuremap

import (
	"context"
	"fmt"
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
		typeName = resourceTypeName(typeName)
		if typeName == "" {
			return map[string]any{}, nil
		}
		if isFHIRResourceType(typeName) {
			return map[string]any{"resourceType": typeName}, nil
		}
		return map[string]any{}, nil
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
			return nil, fmt.Errorf("FHIRPath engine is unavailable for evaluate transform")
		}
		resource := sourceValue
		if resource == nil {
			resource = vars["__root"]
		}
		values, err := engine.EvalWithEnv(ctx, expr, resource, vars)
		if err != nil {
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

func isFHIRResourceType(typeName string) bool {
	if typeName == "" {
		return false
	}
	switch typeName {
	case "Bundle", "Patient", "Observation", "RelatedPerson", "Questionnaire", "QuestionnaireResponse", "Parameters", "Coding", "CodeableConcept", "HumanName", "Quantity", "Reference", "Identifier", "ContactPoint", "Period":
		return true
	default:
		return strings.ToUpper(typeName[:1]) == typeName[:1] && !strings.Contains(typeName, ".")
	}
}
