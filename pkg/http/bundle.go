package http

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func marshalHistoryBundle(basePath, resourceType, id string, versions []store.ResourceVersion) ([]byte, error) {
	entries := make([]map[string]interface{}, 0, len(versions))
	for _, version := range versions {
		entry := map[string]interface{}{
			"fullUrl": historyLocation(basePath, resourceType, id, version.VersionID),
		}
		if version.Resource != nil && len(version.Resource.JSON) > 0 {
			var resourceObj interface{}
			if err := json.Unmarshal(version.Resource.JSON, &resourceObj); err != nil {
				return nil, fmt.Errorf("unmarshal history resource: %w", err)
			}
			entry["resource"] = resourceObj
		}
		method := historyMethod(version.Action)
		entry["request"] = map[string]interface{}{
			"method": method,
			"url":    historyRequestURL(resourceType, id, version.Action),
		}
		entries = append(entries, entry)
	}

	obj := map[string]interface{}{
		"resourceType": "Bundle",
		"type":         "history",
		"entry":        entries,
	}
	return json.Marshal(obj)
}

func historyMethod(action store.VersionAction) string {
	switch action {
	case store.VersionActionCreate:
		return "POST"
	case store.VersionActionUpdate:
		return "PUT"
	case store.VersionActionDelete:
		return "DELETE"
	default:
		return "GET"
	}
}

func historyRequestURL(resourceType, id string, action store.VersionAction) string {
	switch action {
	case store.VersionActionCreate:
		return resourceType
	default:
		return resourceType + "/" + id
	}
}

func marshalSearchBundle(bundle *search.SearchBundle) ([]byte, error) {
	if bundle == nil {
		bundle = &search.SearchBundle{}
	}
	obj := map[string]interface{}{
		"resourceType": "Bundle",
		"type":         "searchset",
	}
	if bundle.Total != nil {
		obj["total"] = *bundle.Total
	}
	if len(bundle.Links) > 0 {
		links := make([]map[string]string, 0, len(bundle.Links))
		for relation, url := range bundle.Links {
			links = append(links, map[string]string{
				"relation": relation,
				"url":      url,
			})
		}
		obj["link"] = links
	}
	entries := make([]map[string]interface{}, 0, len(bundle.Entries))
	for _, entry := range bundle.Entries {
		item := map[string]interface{}{
			"fullUrl": entry.FullURL,
		}
		if entry.Resource != nil && len(entry.Resource.JSON) > 0 {
			var resourceObj interface{}
			if err := json.Unmarshal(entry.Resource.JSON, &resourceObj); err != nil {
				return nil, fmt.Errorf("unmarshal search resource: %w", err)
			}
			item["resource"] = resourceObj
		}
		if entry.Mode != "" {
			item["search"] = map[string]string{"mode": entry.Mode}
		}
		entries = append(entries, item)
	}
	obj["entry"] = entries
	return json.Marshal(obj)
}

func marshalCapabilityStatement(snapshot registry.CapabilitySnapshot, meta ServerMetadata, searchEnabled bool) ([]byte, error) {
	rest := make([]map[string]interface{}, 0, 1)
	resourceEntries := make([]map[string]interface{}, 0, len(snapshot.Resources))
	for _, res := range snapshot.Resources {
		interactions := []map[string]string{
			{"code": "read"},
			{"code": "create"},
			{"code": "update"},
			{"code": "delete"},
			{"code": "history-instance"},
		}
		if searchEnabled {
			interactions = append(interactions, map[string]string{"code": "search-type"})
		}
		resourceEntries = append(resourceEntries, map[string]interface{}{
			"type":         res.ResourceType,
			"interaction":  interactions,
			"searchParam":  searchParamsForCapability(res.SearchParameters),
			"versioning":   "versioned",
			"readHistory":  true,
			"updateCreate": false,
		})
	}
	rest = append(rest, map[string]interface{}{
		"mode":     "server",
		"resource": resourceEntries,
	})

	software := map[string]interface{}{}
	if meta.SoftwareName != "" {
		software["name"] = meta.SoftwareName
	}
	if meta.SoftwareVersion != "" {
		software["version"] = meta.SoftwareVersion
	}

	implementation := map[string]interface{}{}
	if meta.ServerName != "" {
		implementation["description"] = meta.ServerName
	}

	obj := map[string]interface{}{
		"resourceType": "CapabilityStatement",
		"status":       "active",
		"date":         snapshot.CompiledAt.UTC().Format(time.RFC3339),
		"kind":         "instance",
		"fhirVersion":  snapshot.FHIRVersion,
		"format":       []string{"application/fhir+json"},
		"rest":         rest,
	}
	if len(software) > 0 {
		obj["software"] = software
	}
	if len(implementation) > 0 {
		obj["implementation"] = implementation
	}
	if meta.Description != "" {
		obj["description"] = meta.Description
	}
	return json.Marshal(obj)
}

func searchParamsForCapability(params []registry.SearchParameterInfo) []map[string]string {
	if len(params) == 0 {
		return nil
	}
	out := make([]map[string]string, 0, len(params))
	for _, param := range params {
		out = append(out, map[string]string{
			"name": param.Code,
			"type": param.Type,
		})
	}
	return out
}

func isTransactionBundle(data []byte) (bool, error) {
	normalized, err := types.NormalizeJSON(data)
	if err != nil {
		return false, err
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(normalized, &obj); err != nil {
		return false, err
	}
	resourceType, _ := obj["resourceType"].(string)
	if resourceType != "Bundle" {
		return false, nil
	}
	bundleType, _ := obj["type"].(string)
	return bundleType == "transaction", nil
}
