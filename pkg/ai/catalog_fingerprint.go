package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/degoke/haistack/pkg/validate"
)

func canonicalCatalogKey(c *PHICatalog) string {
	if c == nil {
		return "default"
	}
	def := DefaultPHICatalog()
	if catalogsEquivalent(c, def) {
		return "default"
	}
	sum := sha256.Sum256(stableJSON(c))
	return "catalog-" + hex.EncodeToString(sum[:8])
}

func catalogsEquivalent(a, b *PHICatalog) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return string(stableJSON(a)) == string(stableJSON(b))
}

func stableJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("marshal-error")
	}
	return b
}

func canonicalRulesKey(rules *PHIStructureRules) string {
	if rules == nil {
		return "default-rules"
	}
	def := DefaultPHIStructureRules()
	if string(stableJSON(rules)) == string(stableJSON(&def)) {
		return "default-rules"
	}
	sum := sha256.Sum256(stableJSON(rules))
	return "rules-" + hex.EncodeToString(sum[:8])
}

func canonicalEngineKey(engine interface{}) string {
	if engine == nil {
		return "default-engine"
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%T", engine)))
	return "engine-" + hex.EncodeToString(sum[:8])
}

func canonicalProfileCatalogKey(p validate.ProfileCatalog) string {
	if p == nil {
		return "none"
	}
	switch c := p.(type) {
	case validate.MemoryProfileCatalog:
		urls := make([]string, 0, len(c))
		for url := range c {
			urls = append(urls, url)
		}
		sort.Strings(urls)
		var parts []string
		for _, url := range urls {
			sum := sha256.Sum256(stableJSON(c[url]))
			parts = append(parts, url+":"+hex.EncodeToString(sum[:6]))
		}
		return "profiles-" + strings.Join(parts, ";")
	default:
		sum := sha256.Sum256([]byte(fmt.Sprintf("%T", p)))
		return "profiles-" + hex.EncodeToString(sum[:8])
	}
}
