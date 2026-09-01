package validate

import (
	"context"
	"fmt"
	"strings"
)

func validateExtensionPolicy(ctx context.Context, obj map[string]interface{}, sd *StructureDefinition, issues *[]ValidationIssue) {
	walkExtensions(ctx, obj, sd.Type, sd.URL, issues)
}

func walkExtensions(ctx context.Context, node interface{}, path, profileURL string, issues *[]ValidationIssue) {
	if err := ctx.Err(); err != nil {
		return
	}
	switch current := node.(type) {
	case map[string]interface{}:
		for key, value := range current {
			if key == "extension" || key == "modifierExtension" {
				validateExtensionArray(ctx, value, path+"."+key, profileURL, issues)
				continue
			}
			nextPath := key
			if path != "" {
				nextPath = path + "." + key
			}
			walkExtensions(ctx, value, nextPath, profileURL, issues)
		}
	case []interface{}:
		for _, item := range current {
			walkExtensions(ctx, item, path, profileURL, issues)
		}
	}
}

func validateExtensionArray(ctx context.Context, raw interface{}, path, profileURL string, issues *[]ValidationIssue) {
	items, ok := raw.([]interface{})
	if !ok {
		return
	}
	for i, rawItem := range items {
		if err := ctx.Err(); err != nil {
			return
		}
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			continue
		}
		url, _ := item["url"].(string)
		if strings.TrimSpace(url) == "" {
			expr := fmt.Sprintf("%s[%d].url", path, i)
			*issues = append(*issues, issue(
				"required",
				fmt.Sprintf("%s: extension url is required (%s)", expr, profileURL),
				[]string{expr},
			))
		}
		walkExtensions(ctx, item, fmt.Sprintf("%s[%d]", path, i), profileURL, issues)
	}
}
