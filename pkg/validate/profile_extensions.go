package validate

import (
	"fmt"
	"strings"
)

func validateExtensionPolicy(obj map[string]interface{}, sd *StructureDefinition, issues *[]ValidationIssue) {
	walkExtensions(obj, sd.Type, sd.URL, issues)
}

func walkExtensions(node interface{}, path, profileURL string, issues *[]ValidationIssue) {
	switch current := node.(type) {
	case map[string]interface{}:
		for key, value := range current {
			if key == "extension" || key == "modifierExtension" {
				validateExtensionArray(value, path+"."+key, profileURL, issues)
				continue
			}
			nextPath := path
			if path == "" {
				nextPath = key
			} else {
				nextPath = path + "." + key
			}
			walkExtensions(value, nextPath, profileURL, issues)
		}
	case []interface{}:
		for _, item := range current {
			walkExtensions(item, path, profileURL, issues)
		}
	}
}

func validateExtensionArray(raw interface{}, path, profileURL string, issues *[]ValidationIssue) {
	items, ok := raw.([]interface{})
	if !ok {
		return
	}
	for i, rawItem := range items {
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
	}
}
