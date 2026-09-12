package parquetfhir

import (
	"fmt"
	"math/big"
	"strings"
	"time"
)

func enrichAnnotations(row map[string]any, index *elementIndex) error {
	if row == nil || index == nil {
		return nil
	}
	return enrichMap(row, index.resourceType, index)
}

func enrichMap(values map[string]any, sdPath string, index *elementIndex) error {
	for key, raw := range values {
		if raw == nil || isAnnotationField(key) {
			continue
		}
		path := sdPath + "." + key
		el := index.lookup(path, key)
		fhirType := elementType(el, key, index.choiceTypes)

		switch v := raw.(type) {
		case string:
			if isDateLikeType(fhirType) {
				start, end, err := dateRange(v, fhirType)
				if err != nil {
					return fmt.Errorf("%s: %w", key, err)
				}
				values[annotationStartField(key)] = start
				values[annotationEndField(key)] = end
			}
			if isDecimalType(fhirType) {
				numeric, err := decimalBytes(v)
				if err != nil {
					return fmt.Errorf("%s numeric: %w", key, err)
				}
				values[annotationNumericField(key)] = numeric
			}
		case map[string]any:
			if err := enrichQuantityGroup(key, v, values); err != nil {
				return err
			}
			if err := enrichMap(v, path, index); err != nil {
				return err
			}
		case []any:
			for _, item := range v {
				if m, ok := item.(map[string]any); ok {
					if err := enrichMap(m, path, index); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func enrichQuantityGroup(fieldName string, qty map[string]any, parent map[string]any) error {
	if qty == nil {
		return nil
	}
	rawValue, ok := qty["value"]
	if !ok || rawValue == nil {
		return nil
	}
	decimalStr, err := coerceDecimalString(rawValue)
	if err != nil {
		return fmt.Errorf("quantity value: %w", err)
	}
	qty["value"] = decimalStr
	numeric, err := decimalBytes(decimalStr)
	if err != nil {
		return err
	}
	qty[quantityValueNumericField()] = numeric

	canonical := map[string]any{
		"value":  decimalStr,
		"code":   stringOrEmpty(qty["code"]),
		"system": stringOrEmpty(qty["system"]),
		"unit":   stringOrEmpty(qty["unit"]),
	}
	canonical[quantityValueNumericField()] = numeric
	parent[annotationCanonicalField(fieldName)] = canonical
	return nil
}

func dateRange(raw, fhirType string) (time.Time, time.Time, error) {
	switch normalizeFHIRType(fhirType) {
	case "date":
		t, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		end := time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999000000, time.UTC)
		return start, end, nil
	case "dateTime", "instant":
		t, err := parseDateTime(raw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		start := t.UTC()
		end := start.Add(time.Second - time.Millisecond)
		return start, end, nil
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("unsupported date type %q", fhirType)
	}
}

func parseDateTime(raw string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid dateTime %q", raw)
}

func decimalBytes(raw string) ([]byte, error) {
	out := make([]byte, 16)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out, nil
	}
	rat, ok := new(big.Rat).SetString(raw)
	if !ok {
		return nil, fmt.Errorf("invalid decimal %q", raw)
	}
	scale := big.NewRat(1, 1)
	for i := 0; i < 6; i++ {
		scale.Mul(scale, big.NewRat(10, 1))
	}
	scaled := new(big.Rat).Mul(rat, scale)
	if !scaled.IsInt() {
		return nil, fmt.Errorf("decimal %q exceeds scale 6", raw)
	}
	num := scaled.Num()
	bytes := num.Bytes()
	if len(bytes) > 16 {
		return nil, fmt.Errorf("decimal %q exceeds precision", raw)
	}
	copy(out[16-len(bytes):], bytes)
	return out, nil
}

func coerceDecimalString(raw any) (string, error) {
	switch v := raw.(type) {
	case string:
		return v, nil
	case float64:
		return fmt.Sprintf("%g", v), nil
	case float32:
		return fmt.Sprintf("%g", v), nil
	case int:
		return fmt.Sprintf("%d", v), nil
	case int64:
		return fmt.Sprintf("%d", v), nil
	case int32:
		return fmt.Sprintf("%d", v), nil
	default:
		return fmt.Sprint(v), nil
	}
}

func stringOrEmpty(raw any) string {
	if raw == nil {
		return ""
	}
	if s, ok := raw.(string); ok {
		return s
	}
	return fmt.Sprint(raw)
}
