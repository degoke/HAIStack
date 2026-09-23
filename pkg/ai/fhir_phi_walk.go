package ai

import (
	"context"
	"strings"
)

// V3ConfidentialityCodeSystem is the HL7 v3 confidentiality code system used on
// Resource.meta.security.
const V3ConfidentialityCodeSystem = "http://terminology.hl7.org/CodeSystem/v3-ConfidentialityCode"

// deepScrubResource applies catalog-only redaction (no FHIRPath bundle). Prefer
// scrubResourceMerged when a path bundle is available.
func deepScrubResource(resourceType string, root map[string]any, catalog *PHICatalog, placeholder string) []string {
	redactions, err := scrubResourceMerged(context.Background(), resourceType, root, catalog, phiPathBundle{}, nil, placeholder, scrubOptions{})
	if err != nil {
		return nil
	}
	return redactions
}

func (c *PHICatalog) resourceRequiresStrictRedaction(root map[string]any) bool {
	if c == nil {
		c = DefaultPHICatalog()
	}
	meta, ok := root["meta"].(map[string]any)
	if !ok {
		return false
	}
	security, ok := meta["security"].([]any)
	if !ok {
		return false
	}
	for _, item := range security {
		coding, ok := item.(map[string]any)
		if !ok {
			continue
		}
		system, _ := coding["system"].(string)
		code, _ := coding["code"].(string)
		if code == "" {
			continue
		}
		if c.strictSecurityMatch(system, code) {
			return true
		}
	}
	return false
}

func (c *PHICatalog) strictSecurityMatch(system, code string) bool {
	if c.StrictSecurityLabels != nil {
		if codes, ok := c.StrictSecurityLabels[system]; ok {
			for _, want := range codes {
				if want == code {
					return true
				}
			}
		}
	}
	if system == V3ConfidentialityCodeSystem || system == "" {
		for _, want := range c.strictConfidentialityCodes() {
			if want == code {
				return true
			}
		}
	}
	return false
}

func (c *PHICatalog) strictConfidentialityCodes() []string {
	if len(c.StrictConfidentialityCodes) > 0 {
		return c.StrictConfidentialityCodes
	}
	return []string{"R", "V"}
}

func (c *PHICatalog) pathMatches(resourceType, fullPath string) bool {
	fullPath = strings.TrimPrefix(fullPath, ".")
	norm := normalizePathIndexes(fullPath)
	for _, suffix := range c.globalPathSuffixes() {
		if pathHasSuffix(norm, suffix) {
			return true
		}
	}
	if c.ResourcePathSuffixes != nil {
		for _, suffix := range c.ResourcePathSuffixes[resourceType] {
			if pathHasSuffix(norm, suffix) {
				return true
			}
		}
	}
	if c.ResourceElements != nil {
		for _, el := range c.ResourceElements[resourceType] {
			if el == "" {
				continue
			}
			if norm == el || strings.HasPrefix(norm, el+".") {
				return true
			}
		}
	}
	for _, el := range c.GlobalElements {
		if el == "" {
			continue
		}
		if norm == el || strings.HasPrefix(norm, el+".") {
			return true
		}
	}
	return false
}

func (c *PHICatalog) pathAllowedInStrictMode(fullPath, leaf string) bool {
	norm := normalizePathIndexes(fullPath)
	for _, allowed := range c.StrictAllowPathSuffixes {
		if pathHasSuffix(norm, allowed) || leaf == allowed {
			return true
		}
	}
	return c.alwaysAllowedKey(leaf)
}

func pathHasSuffix(path, suffix string) bool {
	if path == suffix {
		return true
	}
	return strings.HasSuffix(path, "."+suffix) || strings.Contains(path, "."+suffix+".")
}

func (c *PHICatalog) passiveSensitiveKey(key, path string) bool {
	if c.PassiveSensitiveKeys != nil {
		if sensitive, ok := c.PassiveSensitiveKeys[key]; ok {
			return sensitive
		}
	}
	if defaultPassiveSensitiveKeys[key] {
		return true
	}
	if strings.HasPrefix(key, "value") && key != "valueCode" && key != "valueCoding" && key != "valueBoolean" {
		return true
	}
	return false
}

func (c *PHICatalog) alwaysAllowedKey(key string) bool {
	switch key {
	case "resourceType", "id", "code", "system", "version", "status", "use", "mode":
		return true
	default:
		return false
	}
}

func (c *PHICatalog) globalPathSuffixes() []string {
	if len(c.GlobalPathSuffixes) > 0 {
		return c.GlobalPathSuffixes
	}
	return defaultGlobalPathSuffixes()
}
