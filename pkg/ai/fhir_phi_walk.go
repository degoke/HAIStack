package ai

import (
	"strings"
)

// V3ConfidentialityCodeSystem is the HL7 v3 confidentiality code system used on
// Resource.meta.security.
const V3ConfidentialityCodeSystem = "http://terminology.hl7.org/CodeSystem/v3-ConfidentialityCode"

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
	return strings.HasSuffix(path, "."+suffix)
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
