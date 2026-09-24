package ai

import (
	"fmt"
	"strings"
)

// provenanceTargetsExplicitInSpecs returns clinical target refs already covered by
// Provenance POST entries in the bundle input (model- or host-supplied).
func provenanceTargetsExplicitInSpecs(specs []transactionEntrySpec) map[string]bool {
	out := map[string]bool{}
	for _, spec := range specs {
		if strings.ToUpper(strings.TrimSpace(spec.Method)) != "POST" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(spec.ResourceType), "Provenance") {
			continue
		}
		for ref := range provenanceTargetRefsFromFields(spec.Fields) {
			out[ref] = true
		}
	}
	return out
}

func provenanceTargetRefsFromFields(fields map[string]any) map[string]bool {
	out := map[string]bool{}
	if len(fields) == 0 {
		return out
	}
	raw, ok := fields["target"]
	if !ok {
		return out
	}
	list, ok := raw.([]any)
	if !ok {
		return out
	}
	for _, item := range list {
		ref := referenceString(item)
		if ref != "" {
			out[normalizeProvenanceTargetRef(ref)] = true
		}
	}
	return out
}

func referenceString(item any) string {
	m, ok := item.(map[string]any)
	if !ok {
		return ""
	}
	if ref := strings.TrimSpace(fmt.Sprint(m["reference"])); ref != "" {
		return ref
	}
	return strings.TrimSpace(fmt.Sprint(m["uri"]))
}

func normalizeProvenanceTargetRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "urn:") {
		return ref
	}
	// Patient/id
	if i := strings.Index(ref, "/"); i > 0 {
		return ref
	}
	return ref
}

func provenanceTargetCovered(explicit map[string]bool, target string) bool {
	if explicit == nil {
		return false
	}
	if explicit[target] {
		return true
	}
	return explicit[normalizeProvenanceTargetRef(target)]
}
