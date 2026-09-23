package ai

import (
	"fmt"
	"strings"
)

// V3ConfidentialityCodeSystem is the HL7 v3 confidentiality code system used on
// Resource.meta.security.
const V3ConfidentialityCodeSystem = "http://terminology.hl7.org/CodeSystem/v3-ConfidentialityCode"

// deepScrubResource walks a FHIR resource JSON map and redacts PHI using deep
// path rules, passive element-name detection, and meta.security labels.
func deepScrubResource(resourceType string, root map[string]any, catalog *PHICatalog, placeholder string) []string {
	if root == nil {
		return nil
	}
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	st := &scrubState{
		resourceType: resourceType,
		catalog:      catalog,
		placeholder:  placeholder,
		strict:       catalog.resourceRequiresStrictRedaction(root),
	}
	st.walkMap(root)
	if contained, ok := root["contained"].([]any); ok {
		for i, item := range contained {
			child, ok := item.(map[string]any)
			if !ok {
				continue
			}
			childType, _ := child["resourceType"].(string)
			childRedactions := deepScrubResource(childType, child, catalog, placeholder)
			st.redactions = append(st.redactions, childRedactions...)
			contained[i] = child
		}
	}
	return uniqueStrings(st.redactions)
}

type scrubState struct {
	resourceType string
	catalog      *PHICatalog
	placeholder  string
	strict       bool
	path         []string
	redactions   []string
}

func (s *scrubState) walkMap(m map[string]any) {
	for key, val := range m {
		if s.redactKey(m, key, val) {
			continue
		}
		s.descend(key, val)
	}
}

func (s *scrubState) redactKey(parent map[string]any, key string, val any) bool {
	fullPath := s.joinPath(append(s.path, key))
	if s.catalog.pathMatches(s.resourceType, fullPath) {
		parent[key] = s.placeholder
		s.redactions = append(s.redactions, fmt.Sprintf("%s.%s", s.resourceType, fullPath))
		return true
	}
	if s.isLeaf(val) {
		if s.catalog.passiveSensitiveKey(key, fullPath) || (s.strict && s.shouldRedactStrictLeaf(key, fullPath)) {
			parent[key] = s.placeholder
			s.redactions = append(s.redactions, fmt.Sprintf("%s.%s", s.resourceType, fullPath))
			return true
		}
	}
	return false
}

func (s *scrubState) descend(key string, val any) {
	s.path = append(s.path, key)
	defer func() { s.path = s.path[:len(s.path)-1] }()

	switch v := val.(type) {
	case map[string]any:
		s.walkMap(v)
	case []any:
		for i, item := range v {
			idx := fmt.Sprintf("%d", i)
			if itemMap, ok := item.(map[string]any); ok {
				s.path = append(s.path, idx)
				s.walkMap(itemMap)
				s.path = s.path[:len(s.path)-1]
				continue
			}
			fullPath := s.joinPath(append(s.path, idx))
			if s.strict && !s.catalog.pathAllowedInStrictMode(fullPath, idx) {
				v[i] = s.placeholder
				s.redactions = append(s.redactions, fmt.Sprintf("%s.%s", s.resourceType, fullPath))
			}
		}
	}
}

func (s *scrubState) isLeaf(val any) bool {
	switch val.(type) {
	case map[string]any, []any:
		return false
	default:
		return true
	}
}

func (s *scrubState) shouldRedactStrictLeaf(key, fullPath string) bool {
	if s.catalog.pathAllowedInStrictMode(fullPath, key) {
		return false
	}
	if s.catalog.alwaysAllowedKey(key) {
		return false
	}
	return true
}

func (s *scrubState) joinPath(parts []string) string {
	var out []string
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, ".")
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
	for _, suffix := range c.globalPathSuffixes() {
		if pathHasSuffix(fullPath, suffix) {
			return true
		}
	}
	if c.ResourcePathSuffixes != nil {
		for _, suffix := range c.ResourcePathSuffixes[resourceType] {
			if pathHasSuffix(fullPath, suffix) {
				return true
			}
		}
	}
	if c.ResourceElements != nil {
		for _, el := range c.ResourceElements[resourceType] {
			if el == "" {
				continue
			}
			if fullPath == el || strings.HasPrefix(fullPath, el+".") {
				return true
			}
		}
	}
	for _, el := range c.GlobalElements {
		if el == "" {
			continue
		}
		if fullPath == el || strings.HasPrefix(fullPath, el+".") {
			return true
		}
	}
	return false
}

func (c *PHICatalog) pathAllowedInStrictMode(fullPath, leaf string) bool {
	for _, allowed := range c.StrictAllowPathSuffixes {
		if pathHasSuffix(fullPath, allowed) || leaf == allowed {
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
