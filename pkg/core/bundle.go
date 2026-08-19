package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// ProcessTransactionBundle executes a FHIR transaction bundle atomically.
//
// Only Bundle.type=transaction with POST, PUT, and DELETE entries is supported.
// Returns a transaction-response Bundle envelope or an error that fails the whole bundle.
func (s *ResourceService) ProcessTransactionBundle(ctx context.Context, bundle *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if bundle == nil {
		return nil, invalidErr("bundle envelope is required", nil)
	}

	parsed, err := parseTransactionBundle(bundle)
	if err != nil {
		return nil, err
	}

	session, err := s.sessions.BeginWrite(ctx)
	if err != nil {
		return nil, exceptionErr("begin write session", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = session.Rollback(ctx)
		}
	}()

	responseEntries := make([]bundleResponseEntry, 0, len(parsed.Entries))
	for _, entry := range parsed.Entries {
		resp, writeErr := s.executeBundleEntry(ctx, session, entry)
		if writeErr != nil {
			return nil, writeErr
		}
		responseEntries = append(responseEntries, resp)
	}

	if err := session.Commit(ctx); err != nil {
		return nil, exceptionErr("commit write session", err)
	}
	committed = true

	responseJSON, err := buildTransactionResponseBundle(responseEntries)
	if err != nil {
		return nil, exceptionErr("build transaction response bundle", err)
	}

	return &types.ResourceEnvelope{
		ResourceType: "Bundle",
		JSON:         responseJSON,
		Hash:         mustHash(responseJSON),
	}, nil
}

type transactionBundle struct {
	Entries []bundleRequestEntry
}

type bundleRequestEntry struct {
	Method      string
	URL         string
	Resource    *types.ResourceEnvelope
	IfMatch     string
	IfNoneExist string
}

type bundleResponseEntry struct {
	Status       string
	Location     string
	ETag         string
	LastModified time.Time
}

func parseTransactionBundle(bundle *types.ResourceEnvelope) (*transactionBundle, error) {
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
	if bundleType != "transaction" {
		return nil, notSupportedErr(fmt.Sprintf("bundle type %q is not supported", bundleType), nil)
	}

	rawEntries, ok := obj["entry"].([]interface{})
	if !ok || len(rawEntries) == 0 {
		return nil, invalidErr("transaction bundle must contain entry", nil, "Bundle.entry")
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
		ifMatch, _ := requestRaw["ifMatch"].(string)
		ifNoneExist, _ := requestRaw["ifNoneExist"].(string)

		switch method {
		case "POST", "PUT", "DELETE":
		default:
			return nil, notSupportedErr(fmt.Sprintf("bundle entry method %q is not supported", method), nil)
		}
		if url == "" {
			return nil, invalidErr(fmt.Sprintf("bundle entry %d missing request url", i), nil, "Bundle.entry.request.url")
		}
		if strings.Contains(url, "?") {
			return nil, notSupportedErr("conditional bundle URLs are not supported", nil)
		}
		if strings.TrimSpace(ifNoneExist) != "" {
			return nil, notSupportedErr("conditional bundle requests are not supported", nil)
		}

		var resourceEnv *types.ResourceEnvelope
		if method != "DELETE" {
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

	return &transactionBundle{Entries: entries}, nil
}

func (s *ResourceService) executeBundleEntry(
	ctx context.Context,
	session store.WriteSession,
	entry bundleRequestEntry,
) (bundleResponseEntry, error) {
	switch entry.Method {
	case "POST":
		return s.executeBundleCreate(ctx, session, entry)
	case "PUT":
		return s.executeBundleUpdate(ctx, session, entry)
	case "DELETE":
		return s.executeBundleDelete(ctx, session, entry)
	default:
		return bundleResponseEntry{}, notSupportedErr(fmt.Sprintf("method %q is not supported", entry.Method), nil)
	}
}

func (s *ResourceService) executeBundleCreate(
	ctx context.Context,
	session store.WriteSession,
	entry bundleRequestEntry,
) (bundleResponseEntry, error) {
	if entry.IfMatch != "" {
		return bundleResponseEntry{}, notSupportedErr("If-Match is not valid for bundle create", nil)
	}
	expectedType := entry.URL
	if strings.Contains(expectedType, "/") {
		return bundleResponseEntry{}, invalidErr("POST url must be a resource type", nil, "Bundle.entry.request.url")
	}

	envelope, err := s.normalizeEnvelope(entry.Resource)
	if err != nil {
		return bundleResponseEntry{}, err
	}
	if envelope.ResourceType == "" {
		envelope.ResourceType = expectedType
	}
	if envelope.ResourceType != expectedType {
		return bundleResponseEntry{}, invalidErr(
			fmt.Sprintf("resourceType mismatch: expected %s, got %s", expectedType, envelope.ResourceType),
			nil,
		)
	}

	if s.validator != nil {
		if err := s.validator.ValidateResource(ctx, envelope); err != nil {
			return bundleResponseEntry{}, invalidErr("resource validation failed", err)
		}
	}

	id := envelope.ID
	if id != "" {
		if err := s.idPolicy.Validate(envelope.ResourceType, id); err != nil {
			return bundleResponseEntry{}, invalidErr("invalid resource id", err, "Resource.id")
		}
	} else {
		generated, err := s.idPolicy.Generate(envelope.ResourceType)
		if err != nil {
			return bundleResponseEntry{}, exceptionErr("generate resource id", err)
		}
		id = generated
	}
	envelope.ID = id
	jsonWithID, err := types.SetID(envelope.JSON, id)
	if err != nil {
		return bundleResponseEntry{}, invalidErr("set resource id", err, "Resource.id")
	}
	envelope.JSON = jsonWithID

	exists, err := session.ResourceStore().Exists(ctx, envelope.ResourceType, id)
	if err != nil {
		return bundleResponseEntry{}, exceptionErr("check resource existence", err)
	}
	if exists {
		return bundleResponseEntry{}, conflictErr(fmt.Sprintf("resource already exists: %s/%s", envelope.ResourceType, id), nil)
	}

	written, err := s.applyWrite(ctx, session, envelope, store.VersionActionCreate)
	if err != nil {
		return bundleResponseEntry{}, err
	}
	return bundleResponseFromWrite("201 Created", written), nil
}

func (s *ResourceService) executeBundleUpdate(
	ctx context.Context,
	session store.WriteSession,
	entry bundleRequestEntry,
) (bundleResponseEntry, error) {
	resourceType, id, err := parseResourceURL(entry.URL)
	if err != nil {
		return bundleResponseEntry{}, err
	}

	envelope, err := s.normalizeEnvelope(entry.Resource)
	if err != nil {
		return bundleResponseEntry{}, err
	}
	if envelope.ResourceType == "" {
		envelope.ResourceType = resourceType
	}
	if envelope.ResourceType != resourceType {
		return bundleResponseEntry{}, invalidErr(
			fmt.Sprintf("resourceType mismatch: expected %s, got %s", resourceType, envelope.ResourceType),
			nil,
		)
	}
	envelope.ID = id
	jsonWithID, err := types.SetID(envelope.JSON, id)
	if err != nil {
		return bundleResponseEntry{}, invalidErr("set resource id", err, "Resource.id")
	}
	envelope.JSON = jsonWithID

	if err := s.idPolicy.Validate(resourceType, id); err != nil {
		return bundleResponseEntry{}, invalidErr("invalid resource id", err, "Resource.id")
	}
	if s.validator != nil {
		if err := s.validator.ValidateResource(ctx, envelope); err != nil {
			return bundleResponseEntry{}, invalidErr("resource validation failed", err)
		}
	}

	exists, err := session.ResourceStore().Exists(ctx, resourceType, id)
	if err != nil {
		return bundleResponseEntry{}, exceptionErr("check resource existence", err)
	}
	if !exists {
		return bundleResponseEntry{}, notFoundErr(fmt.Sprintf("resource not found: %s/%s", resourceType, id), nil)
	}
	if entry.IfMatch != "" {
		expected, ok := versionFromETag(entry.IfMatch)
		if !ok {
			return bundleResponseEntry{}, invalidErr("bundle ifMatch must contain one entity tag", nil)
		}
		current, err := session.ResourceStore().Read(ctx, resourceType, id)
		if err != nil {
			return bundleResponseEntry{}, exceptionErr("read current resource for ifMatch", err)
		}
		if expected != "*" && expected != current.VersionID {
			return bundleResponseEntry{}, preconditionErr(fmt.Sprintf("resource version does not match expected version %q", expected), nil)
		}
		written, err := s.applyWriteExpectedVersion(ctx, session, envelope, store.VersionActionUpdate, expected)
		if err != nil {
			return bundleResponseEntry{}, err
		}
		return bundleResponseFromWrite("200 OK", written), nil
	}

	written, err := s.applyWrite(ctx, session, envelope, store.VersionActionUpdate)
	if err != nil {
		return bundleResponseEntry{}, err
	}
	return bundleResponseFromWrite("200 OK", written), nil
}

func (s *ResourceService) executeBundleDelete(
	ctx context.Context,
	session store.WriteSession,
	entry bundleRequestEntry,
) (bundleResponseEntry, error) {
	resourceType, id, err := parseResourceURL(entry.URL)
	if err != nil {
		return bundleResponseEntry{}, err
	}
	if err := s.idPolicy.Validate(resourceType, id); err != nil {
		return bundleResponseEntry{}, invalidErr("invalid resource id", err, "Resource.id")
	}

	current, err := session.ResourceStore().Read(ctx, resourceType, id)
	if err != nil {
		if isStoreNotFound(err) {
			return bundleResponseEntry{}, notFoundErr(fmt.Sprintf("resource not found: %s/%s", resourceType, id), err)
		}
		return bundleResponseEntry{}, exceptionErr("read resource for delete", err)
	}

	expected := ""
	if entry.IfMatch != "" {
		var ok bool
		expected, ok = versionFromETag(entry.IfMatch)
		if !ok {
			return bundleResponseEntry{}, invalidErr("bundle ifMatch must contain one entity tag", nil)
		}
	}
	if err := s.applyDeleteExpectedVersion(ctx, session, current, expected); err != nil {
		return bundleResponseEntry{}, err
	}
	return bundleResponseEntry{Status: "204 No Content"}, nil
}

func versionFromETag(raw string) (string, bool) {
	parts := strings.Split(raw, ",")
	if len(parts) != 1 {
		return "", false
	}
	tag := strings.TrimSpace(parts[0])
	if tag == "*" {
		return tag, true
	}
	if strings.HasPrefix(tag, "W/") {
		tag = strings.TrimSpace(strings.TrimPrefix(tag, "W/"))
	}
	if len(tag) < 2 || tag[0] != '"' || tag[len(tag)-1] != '"' {
		return "", false
	}
	version := tag[1 : len(tag)-1]
	if version == "" {
		return "", false
	}
	return version, true
}

func parseResourceURL(url string) (resourceType, id string, err error) {
	parts := strings.Split(url, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", invalidErr("url must be ResourceType/id", nil, "Bundle.entry.request.url")
	}
	return parts[0], parts[1], nil
}

func bundleResponseFromWrite(status string, written *types.ResourceEnvelope) bundleResponseEntry {
	return bundleResponseEntry{
		Status:       status,
		Location:     fmt.Sprintf("%s/%s/_history/%s", written.ResourceType, written.ID, written.VersionID),
		ETag:         fmt.Sprintf("W/\"%s\"", written.VersionID),
		LastModified: written.LastUpdated,
	}
}

func buildTransactionResponseBundle(entries []bundleResponseEntry) ([]byte, error) {
	responseEntries := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		response := map[string]interface{}{
			"status": entry.Status,
		}
		if entry.Location != "" {
			response["location"] = entry.Location
		}
		if entry.ETag != "" {
			response["etag"] = entry.ETag
		}
		if !entry.LastModified.IsZero() {
			response["lastModified"] = entry.LastModified.UTC().Format(time.RFC3339)
		}
		responseEntries = append(responseEntries, map[string]interface{}{
			"response": response,
		})
	}

	obj := map[string]interface{}{
		"resourceType": "Bundle",
		"type":         "transaction-response",
		"entry":        responseEntries,
	}
	return json.Marshal(obj)
}

func mustHash(data []byte) string {
	hash, err := types.HashResource(data)
	if err != nil {
		return ""
	}
	return hash
}
