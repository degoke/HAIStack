package ai

// metaProfileURLs returns declared StructureDefinition canonical URLs from
// Resource.meta.profile.
func metaProfileURLs(resource map[string]any) []string {
	if resource == nil {
		return nil
	}
	meta, _ := resource["meta"].(map[string]any)
	if meta == nil {
		return nil
	}
	raw, ok := meta["profile"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []any:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), v...)
	case string:
		if v != "" {
			return []string{v}
		}
	}
	return nil
}
