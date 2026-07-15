package app

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/jackc/pgx/v5"
)

// FHIRPathValuesJSON marshals a FHIRPath result collection as JSON.
func FHIRPathValuesJSON(values []fhirpath.Value) ([]byte, error) {
	items := make([]map[string]any, 0, len(values))
	for _, v := range values {
		item := map[string]any{"type": v.Type()}
		switch v.Type() {
		case "Boolean":
			if b, err := v.Bool(); err == nil {
				item["value"] = b
			}
		case "String":
			if s, err := v.String(); err == nil {
				item["value"] = s
			}
		case "Integer", "Decimal":
			if n, err := v.Float64(); err == nil {
				item["value"] = n
			}
		default:
			item["value"] = v.Type()
		}
		items = append(items, item)
	}
	return json.Marshal(items)
}

// FormatFHIRPathText renders FHIRPath values for human output.
func FormatFHIRPathText(values []fhirpath.Value) string {
	if len(values) == 0 {
		return "[]"
	}
	data, err := FHIRPathValuesJSON(values)
	if err != nil {
		return fmt.Sprintf("%v", values)
	}
	return string(data)
}

// IsNoRows reports whether err is a missing-row database error.
func IsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
