package structuremap

import (
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/degoke/health-ai-stack/pkg/conceptmap"
	"github.com/google/uuid"
)

func (e Engine) applyTransform(ctx context.Context, transform string, params []Parameter, vars map[string]any, sourceValue any) (any, error) {
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
		if len(params) == 1 {
			text, err := parameterLiteral(params[0], vars)
			if err != nil {
				return nil, err
			}
			return map[string]any{"text": text}, nil
		}
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
		if len(coding) == 0 {
			return map[string]any{}, nil
		}
		return map[string]any{"coding": []any{coding}}, nil
	case "c":
		coding := map[string]any{}
		if len(params) > 0 {
			system, err := parameterLiteral(params[0], vars)
			if err != nil {
				return nil, err
			}
			if len(params) > 1 {
				if system != "" {
					coding["system"] = system
				}
				code, err := parameterLiteral(params[1], vars)
				if err != nil {
					return nil, err
				}
				if code != "" {
					coding["code"] = code
				}
			} else if system != "" {
				coding["code"] = system
			}
		}
		if len(params) > 2 {
			display, err := parameterLiteral(params[2], vars)
			if err != nil {
				return nil, err
			}
			if display != "" {
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
		if e.FHIRPath == nil {
			if value, ok := evaluateSimplePath(sourceValue, expr); ok {
				return value, nil
			}
			return nil, fmt.Errorf("evaluate %q: FHIRPath engine is unavailable", expr)
		}
		resource := sourceValue
		if resource == nil {
			resource = vars["__root"]
		}
		values, err := e.FHIRPath.EvalWithEnv(ctx, expr, resource, vars)
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
		identifier := map[string]any{}
		if len(params) > 0 {
			system, err := parameterLiteral(params[0], vars)
			if err != nil {
				return nil, err
			}
			if system != "" {
				identifier["system"] = system
			}
		}
		if len(params) > 1 {
			value, err := parameterLiteral(params[1], vars)
			if err != nil {
				return nil, err
			}
			if value != "" {
				identifier["value"] = value
			}
		}
		if len(params) > 2 {
			typeCode, err := parameterLiteral(params[2], vars)
			if err != nil {
				return nil, err
			}
			if typeCode != "" {
				identifier["type"] = map[string]any{
					"coding": []any{map[string]any{"code": typeCode}},
				}
			}
		}
		return identifier, nil
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
		if len(params) == 0 && sourceValue != nil {
			if ref := resourceReferenceString(asMap(sourceValue)); ref != "" {
				return map[string]any{"reference": ref}, nil
			}
		}
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
	case "pointer":
		if len(params) > 0 {
			value, err := resolveParameter(params[0], vars)
			if err != nil {
				return nil, err
			}
			if ref := resourceReferenceString(asMap(value)); ref != "" {
				return ref, nil
			}
		}
		if ref := resourceReferenceString(asMap(sourceValue)); ref != "" {
			return ref, nil
		}
		return "", nil
	case "truncate":
		if len(params) < 2 {
			return nil, fmt.Errorf("truncate transform requires source and length parameters")
		}
		source, err := resolveParameter(params[0], vars)
		if err != nil {
			return nil, err
		}
		lengthText, err := parameterLiteral(params[1], vars)
		if err != nil {
			return nil, err
		}
		length, err := strconv.Atoi(lengthText)
		if err != nil || length < 0 {
			return nil, fmt.Errorf("truncate length must be a non-negative integer")
		}
		text := fmt.Sprint(source)
		if length >= utf8.RuneCountInString(text) {
			return text, nil
		}
		runes := []rune(text)
		return string(runes[:length]), nil
	case "escape":
		if len(params) < 3 {
			return nil, fmt.Errorf("escape transform requires source, source format, and target format parameters")
		}
		source, err := resolveParameter(params[0], vars)
		if err != nil {
			return nil, err
		}
		fromFmt, err := parameterLiteral(params[1], vars)
		if err != nil {
			return nil, err
		}
		toFmt, err := parameterLiteral(params[2], vars)
		if err != nil {
			return nil, err
		}
		return escapeString(fmt.Sprint(source), fromFmt, toFmt)
	case "dateop":
		return applyDateOp(params, vars, sourceValue)
	case "qty":
		return applyQuantityTransform(params, vars)
	case "cp":
		return applyContactPointTransform(params, vars)
	case "translate":
		return e.applyTranslateTransform(ctx, params, vars, sourceValue)
	default:
		return nil, fmt.Errorf("unsupported StructureMap transform %q", transform)
	}
}

func (e Engine) applyTranslateTransform(ctx context.Context, params []Parameter, vars map[string]any, sourceValue any) (any, error) {
	if e.Translator.Resolver == nil {
		return applyTranslateFallback(params, vars, sourceValue)
	}
	source := codingFromValue(sourceValue)
	mapCanonical := ""
	targetSystem := ""
	switch len(params) {
	case 0:
		return applyTranslateFallback(params, vars, sourceValue)
	case 1:
		mapCanonical, _ = parameterLiteral(params[0], vars)
	default:
		value, err := resolveParameter(params[0], vars)
		if err != nil {
			return nil, err
		}
		source = codingFromValue(value)
		mapCanonical, _ = parameterLiteral(params[1], vars)
		if len(params) > 2 {
			targetSystem, _ = parameterLiteral(params[2], vars)
		}
	}
	if mapCanonical == "" {
		return applyTranslateFallback(params, vars, sourceValue)
	}
	codings, err := e.Translator.Translate(ctx, conceptmap.TranslateRequest{
		MapCanonical: mapCanonical,
		Source:       source,
		TargetSystem: targetSystem,
	})
	if err != nil {
		return nil, err
	}
	if len(codings) == 1 {
		return codings[0], nil
	}
	out := make([]any, len(codings))
	for i, coding := range codings {
		out[i] = coding
	}
	return map[string]any{"coding": out}, nil
}

func applyTranslateFallback(params []Parameter, vars map[string]any, sourceValue any) (any, error) {
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
}

func applyQuantityTransform(params []Parameter, vars map[string]any) (any, error) {
	if len(params) == 0 {
		return map[string]any{}, nil
	}
	if len(params) == 1 {
		text, err := parameterLiteral(params[0], vars)
		if err != nil {
			return nil, err
		}
		return quantityFromText(text), nil
	}
	quantity := map[string]any{}
	if valueText, err := parameterLiteral(params[0], vars); err == nil && valueText != "" {
		if value, err := strconv.ParseFloat(valueText, 64); err == nil {
			quantity["value"] = value
		}
	}
	if len(params) > 1 {
		if unit, err := parameterLiteral(params[1], vars); err == nil && unit != "" {
			quantity["unit"] = unit
		}
	}
	if len(params) > 2 {
		if system, err := parameterLiteral(params[2], vars); err == nil && system != "" {
			quantity["system"] = system
		}
	}
	if len(params) > 3 {
		if code, err := parameterLiteral(params[3], vars); err == nil && code != "" {
			quantity["code"] = code
		}
	}
	return quantity, nil
}

func applyContactPointTransform(params []Parameter, vars map[string]any) (any, error) {
	contact := map[string]any{}
	if len(params) == 1 {
		value, err := parameterLiteral(params[0], vars)
		if err != nil {
			return nil, err
		}
		contact["value"] = value
		if system := inferContactPointSystem(value); system != "" {
			contact["system"] = system
		}
		return contact, nil
	}
	if len(params) > 0 {
		if system, err := parameterLiteral(params[0], vars); err == nil && system != "" {
			contact["system"] = system
		}
	}
	if len(params) > 1 {
		if value, err := parameterLiteral(params[1], vars); err == nil && value != "" {
			contact["value"] = value
		}
	}
	return contact, nil
}

func applyDateOp(params []Parameter, vars map[string]any, sourceValue any) (any, error) {
	if len(params) == 0 {
		return nil, fmt.Errorf("dateOp transform requires a date parameter")
	}
	dateText, err := parameterLiteral(params[0], vars)
	if err != nil {
		return nil, err
	}
	if dateText == "" && sourceValue != nil {
		dateText = fmt.Sprint(sourceValue)
	}
	when, err := parseFHIRDateTime(dateText)
	if err != nil {
		return nil, err
	}
	operation := "add"
	if len(params) > 1 {
		operation, err = parameterLiteral(params[1], vars)
		if err != nil {
			return nil, err
		}
	}
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "add":
		if len(params) < 3 {
			return nil, fmt.Errorf("dateOp add requires a duration parameter")
		}
		duration, err := parameterLiteral(params[2], vars)
		if err != nil {
			return nil, err
		}
		days, err := parseDayDuration(duration)
		if err != nil {
			return nil, err
		}
		return formatFHIRDateTime(when.AddDate(0, 0, days)), nil
	case "today":
		return formatFHIRDate(time.Now()), nil
	case "now":
		return formatFHIRDateTime(time.Now()), nil
	default:
		return nil, fmt.Errorf("unsupported dateOp operation %q", operation)
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

func codingFromValue(value any) map[string]any {
	object := asMap(value)
	if len(object) == 0 {
		return map[string]any{}
	}
	if _, ok := object["code"]; ok {
		return object
	}
	if codings, ok := object["coding"].([]any); ok && len(codings) > 0 {
		if coding, ok := codings[0].(map[string]any); ok {
			return coding
		}
	}
	return object
}

func resourceReferenceString(resource map[string]any) string {
	if len(resource) == 0 {
		return ""
	}
	resourceType, _ := resource["resourceType"].(string)
	id, _ := resource["id"].(string)
	if resourceType != "" && id != "" {
		return resourceType + "/" + id
	}
	if reference, ok := resource["reference"].(string); ok {
		return reference
	}
	return ""
}

func escapeString(source, fromFmt, toFmt string) (string, error) {
	fromFmt = strings.ToLower(strings.TrimSpace(fromFmt))
	toFmt = strings.ToLower(strings.TrimSpace(toFmt))
	plain := unescapeString(source, fromFmt)
	return escapeToFormat(plain, toFmt), nil
}

func unescapeString(source, format string) string {
	switch format {
	case "xml":
		return html.UnescapeString(source)
	case "json":
		unquoted, err := strconv.Unquote("\"" + source + "\"")
		if err != nil {
			return source
		}
		return unquoted
	case "java":
		return strings.NewReplacer(
			"\\n", "\n", "\\r", "\r", "\\t", "\t", "\\\"", "\"", "\\\\", "\\",
		).Replace(source)
	default:
		return source
	}
}

func escapeToFormat(source, format string) string {
	switch format {
	case "xml":
		return html.EscapeString(source)
	case "json":
		return strings.Trim(strconv.Quote(source), "\"")
	case "java":
		return strings.NewReplacer(
			"\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\r", "\\r", "\t", "\\t",
		).Replace(source)
	default:
		return source
	}
}

func quantityFromText(text string) map[string]any {
	text = strings.TrimSpace(text)
	quantity := map[string]any{}
	if text == "" {
		return quantity
	}
	comparator := ""
	if strings.HasPrefix(text, "<=") || strings.HasPrefix(text, ">=") || strings.HasPrefix(text, "<") || strings.HasPrefix(text, ">") {
		switch {
		case strings.HasPrefix(text, "<="):
			comparator = "<="
			text = strings.TrimSpace(text[2:])
		case strings.HasPrefix(text, ">="):
			comparator = ">="
			text = strings.TrimSpace(text[2:])
		case strings.HasPrefix(text, "<"):
			comparator = "<"
			text = strings.TrimSpace(text[1:])
		case strings.HasPrefix(text, ">"):
			comparator = ">"
			text = strings.TrimSpace(text[1:])
		}
	}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return quantity
	}
	if value, err := strconv.ParseFloat(parts[0], 64); err == nil {
		quantity["value"] = value
	}
	if comparator != "" {
		quantity["comparator"] = comparator
	}
	if len(parts) > 1 {
		quantity["unit"] = strings.Join(parts[1:], " ")
	}
	return quantity
}

func inferContactPointSystem(value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "@") {
		return "email"
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return "url"
	}
	if strings.ContainsAny(value, "0123456789") {
		return "phone"
	}
	return ""
}

func parseFHIRDateTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid FHIR date/time %q", value)
}

func formatFHIRDateTime(value time.Time) string {
	if value.Hour() == 0 && value.Minute() == 0 && value.Second() == 0 {
		return value.Format("2006-01-02")
	}
	return value.Format(time.RFC3339)
}

func formatFHIRDate(value time.Time) string {
	return value.Format("2006-01-02")
}

func parseDayDuration(value string) (int, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "P") {
		value = strings.TrimPrefix(value, "P")
		if strings.HasSuffix(value, "D") {
			value = strings.TrimSuffix(value, "D")
		}
	}
	days, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid day duration %q", value)
	}
	return days, nil
}
