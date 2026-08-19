package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/types"
)

type batchResponseEntry struct {
	Status       string
	Location     string
	ETag         string
	LastModified string
	Resource     *types.ResourceEnvelope
	Error        error
}

// ProcessBatchBundle executes a FHIR batch bundle. Each entry is processed
// independently; failures do not roll back successful entries.
func (s *ResourceService) ProcessBatchBundle(ctx context.Context, bundle *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if bundle == nil {
		return nil, invalidErr("bundle envelope is required", nil)
	}

	parsed, err := parseBatchBundle(bundle)
	if err != nil {
		return nil, err
	}

	responses := make([]batchResponseEntry, 0, len(parsed.Entries))
	for _, entry := range parsed.Entries {
		responses = append(responses, s.executeBatchEntry(ctx, entry))
	}

	responseJSON, err := buildBatchResponseBundle(responses)
	if err != nil {
		return nil, exceptionErr("build batch response bundle", err)
	}
	return &types.ResourceEnvelope{
		ResourceType: "Bundle",
		JSON:         responseJSON,
		Hash:         mustHash(responseJSON),
	}, nil
}

type batchBundle struct {
	Entries []bundleRequestEntry
}

func parseBatchBundle(bundle *types.ResourceEnvelope) (*batchBundle, error) {
	normalized, err := types.NormalizeJSON(bundle.JSON)
	if err != nil {
		return nil, invalidErr("normalize bundle JSON", err)
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(normalized, &obj); err != nil {
		return nil, invalidErr("parse bundle JSON", err)
	}

	resourceType, _ := obj["resourceType"].(string)
	if resourceType != "Bundle" {
		return nil, invalidErr("envelope must be a Bundle resource", nil)
	}

	bundleType, _ := obj["type"].(string)
	if bundleType != "batch" {
		return nil, notSupportedErr(fmt.Sprintf("bundle type %q is not supported", bundleType), nil)
	}

	rawEntries, ok := obj["entry"].([]interface{})
	if !ok {
		return nil, invalidErr("batch bundle must contain entry", nil, "Bundle.entry")
	}

	entries := make([]bundleRequestEntry, 0, len(rawEntries))
	for i, raw := range rawEntries {
		entryObj, ok := raw.(map[string]interface{})
		if !ok {
			return nil, invalidErr(fmt.Sprintf("bundle entry %d must be an object", i), nil, "Bundle.entry")
		}

		requestRaw, ok := entryObj["request"].(map[string]interface{})
		if !ok {
			return nil, invalidErr(fmt.Sprintf("bundle entry %d missing request", i), nil, "Bundle.entry.request")
		}

		method, _ := requestRaw["method"].(string)
		method = strings.ToUpper(strings.TrimSpace(method))
		url, _ := requestRaw["url"].(string)
		url = strings.TrimSpace(url)
		ifNoneExist, _ := requestRaw["ifNoneExist"].(string)
		ifMatch, _ := requestRaw["ifMatch"].(string)

		switch method {
		case "GET", "POST", "PUT", "DELETE", "PATCH":
		default:
			return nil, notSupportedErr(fmt.Sprintf("bundle entry method %q is not supported", method), nil)
		}
		if url == "" {
			return nil, invalidErr(fmt.Sprintf("bundle entry %d missing request url", i), nil, "Bundle.entry.request.url")
		}

		var resourceEnv *types.ResourceEnvelope
		if method != "GET" && method != "DELETE" {
			resourceRaw, ok := entryObj["resource"]
			if !ok {
				return nil, invalidErr(fmt.Sprintf("bundle entry %d missing resource", i), nil, "Bundle.entry.resource")
			}
			resourceBytes, err := json.Marshal(resourceRaw)
			if err != nil {
				return nil, invalidErr(fmt.Sprintf("marshal bundle entry %d resource", i), err)
			}
			resourceEnv = &types.ResourceEnvelope{JSON: resourceBytes}
		}

		entries = append(entries, bundleRequestEntry{
			Method:      method,
			URL:         url,
			Resource:    resourceEnv,
			IfMatch:     strings.TrimSpace(ifMatch),
			IfNoneExist: strings.TrimSpace(ifNoneExist),
		})
	}

	return &batchBundle{Entries: entries}, nil
}

func (s *ResourceService) executeBatchEntry(ctx context.Context, entry bundleRequestEntry) batchResponseEntry {
	switch entry.Method {
	case "GET":
		resourceType, id, err := parseResourceURL(entry.URL)
		if err != nil {
			return batchErrorEntry(err)
		}
		read, err := s.Read(ctx, resourceType, id)
		if err != nil {
			return batchErrorEntry(err)
		}
		return batchResponseEntry{
			Status:   "200 OK",
			Resource: read,
		}
	case "POST":
		if entry.IfMatch != "" || entry.IfNoneExist != "" {
			return batchErrorEntry(notSupportedErr("conditional batch create is not supported", nil))
		}
		expectedType := entry.URL
		if strings.Contains(expectedType, "/") {
			return batchErrorEntry(invalidErr("POST url must be a resource type", nil, "Bundle.entry.request.url"))
		}
		envelope, err := s.normalizeEnvelope(entry.Resource)
		if err != nil {
			return batchErrorEntry(err)
		}
		if envelope.ResourceType == "" {
			envelope.ResourceType = expectedType
		}
		if envelope.ResourceType != expectedType {
			return batchErrorEntry(invalidErr(fmt.Sprintf("resourceType mismatch: expected %s, got %s", expectedType, envelope.ResourceType), nil))
		}
		created, err := s.Create(ctx, envelope)
		if err != nil {
			return batchErrorEntry(err)
		}
		resp := bundleResponseFromWrite("201 Created", created)
		return batchResponseEntry{
			Status:       resp.Status,
			Location:     resp.Location,
			ETag:         resp.ETag,
			LastModified: resp.LastModified.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Resource:     created,
		}
	case "PUT":
		if entry.IfNoneExist != "" {
			return batchErrorEntry(notSupportedErr("If-None-Exist is not valid for batch update", nil))
		}
		resourceType, id, err := parseResourceURL(entry.URL)
		if err != nil {
			return batchErrorEntry(err)
		}
		envelope, err := s.normalizeEnvelope(entry.Resource)
		if err != nil {
			return batchErrorEntry(err)
		}
		envelope.ResourceType = resourceType
		envelope.ID = id
		var updated *types.ResourceEnvelope
		if entry.IfMatch != "" {
			expected, ok := versionFromETag(entry.IfMatch)
			if !ok {
				return batchErrorEntry(invalidErr("batch ifMatch must contain one entity tag", nil))
			}
			updated, err = s.UpdateIfMatch(ctx, envelope, expected)
		} else {
			updated, err = s.Update(ctx, envelope)
		}
		if err != nil {
			return batchErrorEntry(err)
		}
		resp := bundleResponseFromWrite("200 OK", updated)
		return batchResponseEntry{
			Status:       resp.Status,
			Location:     resp.Location,
			ETag:         resp.ETag,
			LastModified: resp.LastModified.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Resource:     updated,
		}
	case "DELETE":
		if entry.IfNoneExist != "" {
			return batchErrorEntry(notSupportedErr("If-None-Exist is not valid for batch delete", nil))
		}
		resourceType, id, err := parseResourceURL(entry.URL)
		if err != nil {
			return batchErrorEntry(err)
		}
		var deleteErr error
		if entry.IfMatch != "" {
			expected, ok := versionFromETag(entry.IfMatch)
			if !ok {
				return batchErrorEntry(invalidErr("batch ifMatch must contain one entity tag", nil))
			}
			deleteErr = s.DeleteIfMatch(ctx, resourceType, id, expected)
		} else {
			deleteErr = s.Delete(ctx, resourceType, id)
		}
		if deleteErr != nil {
			return batchErrorEntry(deleteErr)
		}
		return batchResponseEntry{Status: "204 No Content"}
	case "PATCH":
		if entry.IfNoneExist != "" {
			return batchErrorEntry(notSupportedErr("If-None-Exist is not valid for batch patch", nil))
		}
		resourceType, id, err := parseResourceURL(entry.URL)
		if err != nil {
			return batchErrorEntry(err)
		}
		var patched *types.ResourceEnvelope
		if entry.IfMatch != "" {
			expected, ok := versionFromETag(entry.IfMatch)
			if !ok {
				return batchErrorEntry(invalidErr("batch ifMatch must contain one entity tag", nil))
			}
			patched, err = s.PatchIfMatch(ctx, resourceType, id, entry.Resource.JSON, expected)
		} else {
			patched, err = s.Patch(ctx, resourceType, id, entry.Resource.JSON)
		}
		if err != nil {
			return batchErrorEntry(err)
		}
		resp := bundleResponseFromWrite("200 OK", patched)
		return batchResponseEntry{
			Status:       resp.Status,
			Location:     resp.Location,
			ETag:         resp.ETag,
			LastModified: resp.LastModified.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Resource:     patched,
		}
	default:
		return batchErrorEntry(notSupportedErr(fmt.Sprintf("method %q is not supported", entry.Method), nil))
	}
}

func batchErrorEntry(err error) batchResponseEntry {
	if err == nil {
		err = exceptionErr("unknown batch entry error", nil)
	}
	return batchResponseEntry{
		Status: statusFromError(err),
		Error:  err,
	}
}

func statusFromError(err error) string {
	switch KindOf(err) {
	case ErrorKindInvalid:
		return "400 Bad Request"
	case ErrorKindNotFound:
		return "404 Not Found"
	case ErrorKindConflict:
		return "409 Conflict"
	case ErrorKindPrecondition:
		return "412 Precondition Failed"
	case ErrorKindNotSupported:
		return "400 Bad Request"
	default:
		return "500 Internal Server Error"
	}
}

func buildBatchResponseBundle(entries []batchResponseEntry) ([]byte, error) {
	responseEntries := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		item := map[string]interface{}{
			"response": map[string]interface{}{
				"status": entry.Status,
			},
		}
		resp := item["response"].(map[string]interface{})
		if entry.Location != "" {
			resp["location"] = entry.Location
		}
		if entry.ETag != "" {
			resp["etag"] = entry.ETag
		}
		if entry.LastModified != "" {
			resp["lastModified"] = entry.LastModified
		}
		if entry.Error != nil {
			outcome := OperationOutcomeFromError(entry.Error)
			outcomeBytes, err := json.Marshal(outcome)
			if err != nil {
				return nil, err
			}
			var outcomeObj interface{}
			if err := json.Unmarshal(outcomeBytes, &outcomeObj); err != nil {
				return nil, err
			}
			resp["outcome"] = outcomeObj
		}
		if entry.Resource != nil && len(entry.Resource.JSON) > 0 {
			var resourceObj interface{}
			if err := json.Unmarshal(entry.Resource.JSON, &resourceObj); err != nil {
				return nil, err
			}
			item["resource"] = resourceObj
		}
		responseEntries = append(responseEntries, item)
	}

	obj := map[string]interface{}{
		"resourceType": "Bundle",
		"type":         "batch-response",
		"entry":        responseEntries,
	}
	return json.Marshal(obj)
}
