package fhirpath

import (
	"net/url"
	"strings"
)

// ParseReferenceForRead extracts a resource type and id suitable for store.Read.
// It accepts typed relative references, absolute FHIR REST URLs, and urn:uuid
// references when the UUID is used directly as the logical id.
func ParseReferenceForRead(raw, baseURL string) (resourceType, id string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	if strings.HasPrefix(raw, "#") {
		return "", "", false
	}
	if strings.HasPrefix(raw, "urn:uuid:") {
		id = strings.TrimPrefix(raw, "urn:uuid:")
		if id == "" {
			return "", "", false
		}
		return "", id, true
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return parseAbsoluteReferenceURL(raw, baseURL)
	}
	return parseTypedReference(raw)
}

func parseAbsoluteReferenceURL(raw, baseURL string) (resourceType, id string, ok bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	path := strings.Trim(parsed.Path, "/")
	if baseURL != "" {
		basePath := strings.Trim(strings.TrimSpace(baseURL), "/")
		if idx := strings.Index(basePath, "://"); idx >= 0 {
			if baseParsed, err := url.Parse(baseURL); err == nil {
				basePath = strings.Trim(baseParsed.Path, "/")
			}
		}
		if basePath != "" && strings.HasPrefix(path, basePath+"/") {
			path = strings.TrimPrefix(path, basePath+"/")
		}
	}
	segments := strings.Split(path, "/")
	for i := 0; i+1 < len(segments); i++ {
		candidateType := segments[i]
		candidateID := segments[i+1]
		if candidateID == "_history" || candidateID == "" {
			continue
		}
		if !validReferenceResourceType(candidateType) {
			continue
		}
		if candidateID == "_history" && i+2 < len(segments) {
			continue
		}
		return candidateType, candidateID, true
	}
	return "", "", false
}

func parseTypedReference(raw string) (resourceType, id string, ok bool) {
	parts := strings.SplitN(raw, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(parts[0], ":") {
		return "", "", false
	}
	if !validReferenceResourceType(parts[0]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func validReferenceResourceType(resourceType string) bool {
	if resourceType == "" {
		return false
	}
	for i, r := range resourceType {
		if i == 0 && (r < 'A' || r > 'Z') {
			return false
		}
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
