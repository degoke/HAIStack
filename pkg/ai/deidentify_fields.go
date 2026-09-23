package ai

import "encoding/json"

// mergeDeidentifyMetaFields copies meta.security and meta.profile from the full
// resource JSON into filtered so strict mode and profile-aware path bundles work
// during de-identification. Call before Deidentify when policy uses a narrow
// AllowedFields projection.
func mergeDeidentifyMetaFields(filtered map[string]any, fullJSON []byte) error {
	if filtered == nil || len(fullJSON) == 0 {
		return nil
	}
	var fullRoot map[string]any
	if err := json.Unmarshal(fullJSON, &fullRoot); err != nil {
		return err
	}
	fullMeta, ok := fullRoot["meta"].(map[string]any)
	if !ok || len(fullMeta) == 0 {
		return nil
	}
	meta, ok := filtered["meta"].(map[string]any)
	if !ok {
		meta = make(map[string]any)
		filtered["meta"] = meta
	}
	if v, ok := fullMeta["security"]; ok {
		meta["security"] = v
	}
	if v, ok := fullMeta["profile"]; ok {
		meta["profile"] = v
	}
	return nil
}

// projectResourceMap keeps only resourceType, id, and allowed top-level fields.
func projectResourceMap(root map[string]any, allowedFields []string) map[string]any {
	if len(allowedFields) == 0 {
		return root
	}
	out := map[string]any{
		"resourceType": root["resourceType"],
	}
	if id, ok := root["id"]; ok {
		out["id"] = id
	}
	for _, field := range allowedFields {
		if v, ok := root[field]; ok {
			out[field] = v
		}
	}
	return out
}

// projectSearchResourceMap applies search field policy after de-identification.
func projectSearchResourceMap(root map[string]any, allowedFields []string, allowAll bool) map[string]any {
	if allowAll {
		return root
	}
	if len(allowedFields) > 0 {
		return projectResourceMap(root, allowedFields)
	}
	out := map[string]any{
		"resourceType": root["resourceType"],
	}
	if id, ok := root["id"]; ok {
		out["id"] = id
	}
	return out
}

// projectSearchResultData reapplies search/included field projections after de-id.
func projectSearchResultData(
	data any,
	allowAll bool,
	searchAllowed []string,
	includedAllowed [][]string,
) any {
	root, ok := data.(map[string]any)
	if !ok {
		return data
	}
	if res, ok := root["resources"].([]any); ok {
		for i, item := range res {
			if m, ok := item.(map[string]any); ok {
				res[i] = projectSearchResourceMap(m, searchAllowed, allowAll)
			}
		}
	}
	if inc, ok := root["included"].([]any); ok {
		for i, item := range inc {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			allowed := searchAllowed
			if i < len(includedAllowed) {
				allowed = includedAllowed[i]
			}
			inc[i] = projectSearchResourceMap(m, allowed, false)
		}
	}
	return root
}
