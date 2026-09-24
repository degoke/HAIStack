package ai

import (
	"strings"

	"github.com/degoke/haistack/pkg/validate"
)

// PHIStructureRules controls how StructureDefinition elements become FHIRPath
// redaction expressions.
type PHIStructureRules struct {
	// SensitiveTypeCodes marks complex or primitive types that imply redaction.
	SensitiveTypeCodes map[string]bool
	// SensitiveExtensionURLs marks elementdefinition extensions that force redaction.
	SensitiveExtensionURLs map[string]bool
	// SensitiveExtensionSubstrings match extension URLs (case-insensitive contains).
	SensitiveExtensionSubstrings []string
	// RedactMustSupportPrimitives redacts mustSupport elements with string-like types.
	RedactMustSupportPrimitives bool
	// RedactIsSummaryPrimitives redacts isSummary elements with string-like types.
	RedactIsSummaryPrimitives bool
}

// DefaultPHIStructureRules returns built-in StructureDefinition sensitivity rules.
func DefaultPHIStructureRules() PHIStructureRules {
	return PHIStructureRules{
		SensitiveTypeCodes: defaultSensitiveFHIRTypes(),
		SensitiveExtensionURLs: map[string]bool{
			"http://hl7.org/fhir/StructureDefinition/elementdefinition-questionnaire": true,
			"http://hl7.org/fhir/uv/security/StructureDefinition/confidentiality":     true,
		},
		SensitiveExtensionSubstrings: []string{
			"security", "confidentiality", "sensitivity", "phi", "pii",
		},
		RedactMustSupportPrimitives: true,
		RedactIsSummaryPrimitives:   true,
	}
}

func defaultSensitiveFHIRTypes() map[string]bool {
	return map[string]bool{
		"HumanName": true, "Address": true, "ContactPoint": true, "Identifier": true,
		"Attachment": true, "Reference": true, "Period": true,
		"string": true, "markdown": true, "uri": true, "url": true,
		"date": true, "dateTime": true, "instant": true, "time": true,
		"base64Binary": true,
	}
}

// SensitiveFHIRPathsFromStructureDefinition derives FHIRPath expressions from one
// StructureDefinition only. Catalog paths are added once in the path index build.
func SensitiveFHIRPathsFromStructureDefinition(sd *validate.StructureDefinition, rules PHIStructureRules, _ *PHICatalog) []string {
	return sensitiveFHIRPathsFromStructureDefinition(sd, rules)
}

func sensitiveFHIRPathsFromStructureDefinition(sd *validate.StructureDefinition, rules PHIStructureRules) []string {
	if sd == nil || sd.Type == "" {
		return nil
	}
	if rules.SensitiveTypeCodes == nil {
		rules.SensitiveTypeCodes = defaultSensitiveFHIRTypes()
	}
	seen := make(map[string]struct{})
	var out []string

	for _, el := range sd.Elements {
		expr := elementPathToFHIRPath(sd.Type, el.Path)
		if expr == "" {
			continue
		}
		if !elementSensitive(el, rules) {
			continue
		}
		if _, ok := seen[expr]; ok {
			continue
		}
		seen[expr] = struct{}{}
		out = append(out, expr)
	}
	return out
}

func elementPathToFHIRPath(resourceType, path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == resourceType {
		return ""
	}
	path = strings.ReplaceAll(path, ":", ".")
	if strings.HasPrefix(path, resourceType+".") {
		return strings.TrimPrefix(path, resourceType+".")
	}
	return path
}

func elementSensitive(el validate.ElementDefinition, rules PHIStructureRules) bool {
	for _, url := range el.ExtensionURLs {
		if rules.SensitiveExtensionURLs != nil && rules.SensitiveExtensionURLs[url] {
			return true
		}
		lower := strings.ToLower(url)
		for _, sub := range rules.SensitiveExtensionSubstrings {
			if sub != "" && strings.Contains(lower, strings.ToLower(sub)) {
				return true
			}
		}
	}
	for _, typ := range el.Types {
		if rules.SensitiveTypeCodes[typ] {
			return true
		}
	}
	if rules.RedactMustSupportPrimitives && el.MustSupport && hasPrimitiveType(el.Types, rules.SensitiveTypeCodes) {
		return true
	}
	if rules.RedactIsSummaryPrimitives && el.IsSummary && hasPrimitiveType(el.Types, rules.SensitiveTypeCodes) {
		return true
	}
	return false
}

func hasPrimitiveType(types []string, codes map[string]bool) bool {
	primitives := []string{"string", "markdown", "uri", "url", "date", "dateTime", "instant", "time", "base64Binary"}
	for _, typ := range types {
		if codes[typ] {
			for _, p := range primitives {
				if typ == p {
					return true
				}
			}
		}
	}
	return false
}
